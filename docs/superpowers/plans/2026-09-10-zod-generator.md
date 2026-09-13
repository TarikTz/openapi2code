# Zod Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Zod v3 schema generator (`internal/gen/zod`) consuming the existing `internal/ir`, wire it into `pkg/engine` (`GenerateZod`) and the CLI (`--target zod`), matching the PRD's "Runtime Validation Schemas (Zod)" feature.

**Architecture:** `internal/gen/zod` mirrors `internal/gen/ts`'s shape (`ModelOutput{Name, Declaration, Dependencies}`, `Generate(doc) ([]ModelOutput, error)`) and reuses `ts.SanitizeIdentifier`/`ts.QuoteFieldName` directly so both generators agree on identifier handling. Non-cyclic models use `z.infer`; cyclic models (per `ir.Model.Cyclic`) get a hand-rendered TS type plus `z.lazy()`/`z.ZodType<X>` — self-contained within the Zod generator's own output, no dependency on the TS generator's output files.

**Tech Stack:** Go standard library only, reusing already-vendored `gopkg.in/yaml.v3` (test fixtures) — no new dependencies. Generated code targets Zod v3 (the consumer's `package.json`, not this repo, needs the `zod` npm package).

**Spec:** `docs/superpowers/specs/2026-09-10-zod-generator-design.md`

## Global Constraints

- Zod and TS generators are independent: `internal/gen/zod` does not import from or depend on `internal/gen/ts`'s *output*, only reuses two of its exported Go helper functions (`SanitizeIdentifier`, `QuoteFieldName`) at generation time.
- Schema constants are named `<Name>Schema`; the inferred/declared TS type uses the plain model name (`export type Pet = z.infer<typeof PetSchema>;`).
- Non-cyclic models: `export const XSchema = <expr>; export type X = z.infer<typeof XSchema>;`. Cyclic models: `export type X = <tsExpr>; export const XSchema: z.ZodType<X> = z.lazy(() => <zodExpr>);` — both forms self-contained in the same file.
- Nullable and Optional compose independently and in that order: `.nullable()` before `.optional()` when both apply.
- Both output modes exist: monolithic (single `"index.ts"` file) and modular (one `"<Name>.ts"` file per model + barrel `"index.ts"`), mirroring `GenerateTS`'s exact layout contract. Every generated file that uses `z.*` calls (i.e. every model file, and the monolithic file) starts with `import { z } from "zod";`.
- Modular cross-file imports for a model's dependencies import both the schema constant and the type: `import { XSchema, type X } from "./X";` — needed because a cyclic model's hand-rendered TS type references plain type names, while its Zod body references schema constants; importing both uniformly avoids tracking which form each dependent needs.
- `--target zod` is single-target only (not comma-separated `--target ts,zod`).
- No change to `internal/gen/ts`'s output, `pkg/engine.GenerateTS`, or any previously-committed golden file.

---

## Task 1: Fix cycle detection to follow `allOf`-extends edges

**Files:**
- Modify: `internal/ir/build.go`
- Modify: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `cycleWalk.refReachesStart` (already exists, unchanged).
- Produces: `cycleWalk.reachesStart`'s `KindObject` case now also walks `Object.Extends`, so `Model.Cyclic` is correctly set for cycles formed purely through `allOf` inheritance (no later task depends on new exported symbols — this is a pure bugfix to existing logic).

- [ ] **Step 1: Write the failing test**

Add to `internal/ir/build_test.go`:

```go
func TestBuild_AllOfExtendsCycleMarked(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"A": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/B"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"extra": {Type: "string"}}},
				},
			},
			"B": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"a": {Ref: "#/components/schemas/A"},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, m := range doc.Models {
		if !m.Cyclic {
			t.Errorf("expected %s to be marked Cyclic", m.Name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — `A`'s cycle runs entirely through `allOf`-extends (A extends B, B has a field ref back to A), which the current `reachesStart` doesn't walk for `KindObject`, so both models stay `Cyclic: false`.

- [ ] **Step 3: Write the implementation**

In `internal/ir/build.go`, update `cycleWalk.reachesStart`'s `KindObject` case (the only change in this task — nothing else in the function changes):

```go
	case KindObject:
		for _, extend := range node.Object.Extends {
			if w.refReachesStart(extend) {
				return true
			}
		}
		for _, f := range node.Object.Fields {
			if w.reachesStart(f.Type) {
				return true
			}
		}
```

This reuses `refReachesStart` (with its existing memoization) for `Extends` names exactly the way `KindRef` nodes already are — purely additive, so it can only newly detect cycles that were previously missed, never mis-detect an acyclic graph as cyclic, and since no existing generator consults `Model.Cyclic` yet in a way that changes output (the TS generator never reads it), no previously-committed golden file changes as a result.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go
git commit -m "fix: detect cycles formed through allOf-extends edges, not just fields"
```

---

## Task 2: Zod generator core — primitives, objects, refs, identifiers

**Files:**
- Modify: `internal/gen/ts/identifier.go` (export `quoteFieldName` as `QuoteFieldName`)
- Modify: `internal/gen/ts/generate.go` (update its two call sites to the exported name)
- Create: `internal/gen/zod/generate.go`
- Test: `internal/gen/zod/generate_test.go`

**Interfaces:**
- Consumes: `ts.SanitizeIdentifier`, `ts.QuoteFieldName` (newly exported this task), `ir.Document`, `ir.Model`, `ir.Node`, `ir.Kind`, `ir.Primitive`, `ir.ObjectNode`, `ir.Field` (all pre-existing).
- Produces: `zod.ModelOutput{Name, Declaration, Dependencies}`, `zod.Generate(doc *ir.Document) ([]ModelOutput, error)`. This task's `renderModel` does NOT yet check `Model.Cyclic` — Task 5 adds that branch. Every model, cyclic or not, gets the plain `z.infer` treatment for now (Task 5's tests will supersede/extend this).

- [ ] **Step 1: Export `QuoteFieldName` from `internal/gen/ts` (prerequisite)**

In `internal/gen/ts/identifier.go`, rename `quoteFieldName` to `QuoteFieldName` (the function body and doc comment are unchanged apart from the name — it stays the wire-name-preserving quoting helper, now reusable by other generator packages):

```go
// QuoteFieldName renders an object property name. Property names are part
// of the wire contract, so they are never renamed: a name that is not a
// bare-identifier-safe string is quoted (`"content-type"`) rather than
// sanitized, which would silently change the field a consumer must read.
// Exported for reuse by other generator packages (e.g. zod) that need the
// same wire-fidelity guarantee.
func QuoteFieldName(name string) string {
	if isValidIdentifier(name) {
		return name
	}
	return strconv.Quote(name)
}
```

In `internal/gen/ts/generate.go`, update its two call sites (in `renderInterface` and `renderInlineObjectFields`) from `quoteFieldName(f.Name)` to `QuoteFieldName(f.Name)`. No other change to that file.

Run: `go test ./internal/gen/ts/...`
Expected: PASS (pure rename, all existing tests keep passing unchanged — this confirms the rename didn't break anything before moving on).

- [ ] **Step 2: Write the failing tests**

Create `internal/gen/zod/generate_test.go`:

```go
package zod_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/zod"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

func TestGenerate_SimpleObject(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Pet",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "id", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveInteger}, Optional: true},
							{Name: "name", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	want := "export const PetSchema = z.object({ id: z.number().optional(), name: z.string(), });\n" +
		"export type Pet = z.infer<typeof PetSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_NullableAndOptionalField(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Account",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "note", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false, Nullable: true},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const AccountSchema = z.object({ note: z.string().nullable(), });\n" +
		"export type Account = z.infer<typeof AccountSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_RefFieldTracksDependency(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Pet",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "category", Type: &ir.Node{Kind: ir.KindRef, RefName: "Category"}, Optional: true},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const PetSchema = z.object({ category: CategorySchema.optional(), });\n" +
		"export type Pet = z.infer<typeof PetSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Category" {
		t.Errorf("expected Dependencies [Category], got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_PrimitiveAlias(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "PetId", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const PetIdSchema = z.string();\nexport type PetId = z.infer<typeof PetIdSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_RefsToCollidingNamesResolveToUniqueNames(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "Pet-Status", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
			{Name: "Pet.Status", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if outputs[0].Name == outputs[1].Name {
		t.Fatalf("expected distinct names, both got %q", outputs[0].Name)
	}
	if outputs[0].Name != "Pet_Status" || outputs[1].Name != "Pet_Status_2" {
		t.Errorf("got names %q, %q; want Pet_Status, Pet_Status_2", outputs[0].Name, outputs[1].Name)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/gen/zod/...`
Expected: FAIL — package `zod` does not exist.

- [ ] **Step 4: Write the implementation**

Create `internal/gen/zod/generate.go`:

```go
package zod

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Zod schema declaration (plus its
// z.infer'd or hand-written TS type, per model).
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
}

// Generate renders every model in doc as a Zod schema declaration, in the
// same order as doc.Models (name-sorted, so output is deterministic).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	r := &renderer{names: assignNames(doc.Models)}
	outputs := make([]ModelOutput, 0, len(doc.Models))
	for _, m := range doc.Models {
		out, err := r.renderModel(m)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", m.Name, err)
		}
		out.Dependencies = removeName(out.Dependencies, out.Name)
		outputs = append(outputs, out)
	}
	return outputs, nil
}

// barrelName is the modular barrel file's base name. Reserved so a schema
// of the same name cannot claim index.ts and clobber the barrel.
const barrelName = "index"

// assignNames maps each model's IR name to the identifier its schema
// constant and inferred type are declared under. This intentionally
// mirrors internal/gen/ts's assignNames exactly (same SanitizeIdentifier
// call, same reserved barrel name, same _2/_3 collision suffixing in
// doc.Models order) so a spec generating both TS and Zod output gets
// identically-named models across targets.
func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{barrelName: true}
	names := make(map[string]string, len(models))
	for _, m := range models {
		base := ts.SanitizeIdentifier(m.Name)
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		used[name] = true
		names[m.Name] = name
	}
	return names
}

// renderer carries the model-name assignment so every reference to a
// model — its declaration and refs to it — uses the same uniquified
// identifier.
type renderer struct {
	names map[string]string
}

// nameFor returns the identifier a model name is declared under. Names
// not belonging to a model in the document fall back to plain
// sanitization.
func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return ts.SanitizeIdentifier(modelName)
}

func (r *renderer) renderModel(m *ir.Model) (ModelOutput, error) {
	name := r.nameFor(m.Name)
	expr, deps := r.renderZodType(m.Type)
	decl := fmt.Sprintf("export const %sSchema = %s;\nexport type %s = z.infer<typeof %sSchema>;\n", name, expr, name, name)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}, nil
}

func (r *renderer) renderZodType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := r.nameFor(node.RefName)
		return name + "Schema", []string{name}
	case ir.KindEnum:
		return renderEnum(node.Enum), nil
	case ir.KindArray:
		return r.renderArray(node.Array)
	case ir.KindUnion:
		return r.renderUnion(node.Union)
	case ir.KindIntersection:
		return r.renderIntersection(node.Intersection)
	case ir.KindObject:
		return r.renderObject(node.Object)
	default:
		return "z.unknown()", nil
	}
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "z.string()"
	case ir.PrimitiveNumber, ir.PrimitiveInteger:
		return "z.number()"
	case ir.PrimitiveBoolean:
		return "z.boolean()"
	default:
		return "z.unknown()"
	}
}

func renderEnum(e *ir.EnumNode) string {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		parts[i] = strconv.Quote(v)
	}
	return fmt.Sprintf("z.enum([%s])", strings.Join(parts, ", "))
}

func (r *renderer) renderArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := r.renderZodType(a.Items)
	return fmt.Sprintf("z.array(%s)", itemExpr), deps
}

// renderUnion renders oneOf/anyOf. A single-variant union (legal but
// unusual) is rendered as that variant directly — z.union's TS signature
// requires at least two members in its tuple type.
func (r *renderer) renderUnion(u *ir.UnionNode) (string, []string) {
	if len(u.Variants) == 1 {
		return r.renderZodType(u.Variants[0])
	}
	parts := make([]string, len(u.Variants))
	var deps []string
	for i, v := range u.Variants {
		expr, d := r.renderZodType(v)
		parts[i] = expr
		deps = append(deps, d...)
	}
	return fmt.Sprintf("z.union([%s])", strings.Join(parts, ", ")), deps
}

// renderIntersection renders the allOf-with-non-object-member fallback,
// folding 3+ members left-to-right: z.intersection(z.intersection(a, b), c).
func (r *renderer) renderIntersection(in *ir.IntersectionNode) (string, []string) {
	if len(in.Members) == 0 {
		return "z.unknown()", nil
	}
	expr, deps := r.renderZodType(in.Members[0])
	for _, m := range in.Members[1:] {
		next, d := r.renderZodType(m)
		deps = append(deps, d...)
		expr = fmt.Sprintf("z.intersection(%s, %s)", expr, next)
	}
	return expr, deps
}

// renderObject renders an object schema. Extends (from allOf) chains
// .extend() calls: ASchema.extend(BSchema.shape).extend({ own fields }) —
// the own-fields .extend() is omitted entirely when there are no own
// fields, so a pure re-export of a base schema doesn't get a redundant
// empty .extend({}).
func (r *renderer) renderObject(o *ir.ObjectNode) (string, []string) {
	var deps []string
	base := ""
	for i, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		if i == 0 {
			base = name + "Schema"
		} else {
			base = fmt.Sprintf("%s.extend(%sSchema.shape)", base, name)
		}
	}
	fieldsExpr, fieldDeps := r.renderFields(o.Fields)
	deps = append(deps, fieldDeps...)
	if base == "" {
		return fmt.Sprintf("z.object(%s)", fieldsExpr), deps
	}
	if len(o.Fields) == 0 {
		return base, deps
	}
	return fmt.Sprintf("%s.extend(%s)", base, fieldsExpr), deps
}

func (r *renderer) renderFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := r.renderZodType(f.Type)
		deps = append(deps, fieldDeps...)
		if f.Nullable {
			fieldExpr += ".nullable()"
		}
		if f.Optional {
			fieldExpr += ".optional()"
		}
		sb.WriteString(fmt.Sprintf("%s: %s,", ts.QuoteFieldName(f.Name), fieldExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

func dedupeSorted(names []string) []string {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func removeName(names []string, exclude string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != exclude {
			out = append(out, n)
		}
	}
	return out
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/gen/zod/...`
Expected: PASS

Run: `go test ./...`
Expected: PASS (confirms the `QuoteFieldName` export/rename didn't break `internal/gen/ts`'s or anything downstream's tests)

- [ ] **Step 6: Commit**

```bash
git add internal/gen/ts/identifier.go internal/gen/ts/generate.go internal/gen/zod/generate.go internal/gen/zod/generate_test.go
git commit -m "feat: add Zod generator core (primitives, objects, refs, identifiers)"
```

---

## Task 3: Zod generator — enum and array coverage

**Files:**
- Modify: `internal/gen/zod/generate_test.go`

**Interfaces:**
- Consumes: `renderEnum`, `renderArray` (already written in Task 2; this task only adds test coverage — `renderZodType`'s `KindEnum`/`KindArray` cases already dispatch to them, mirroring how `internal/gen/ts`'s Task 10 worked).

- [ ] **Step 1: Write the failing tests**

Add to `internal/gen/zod/generate_test.go`:

```go
func TestGenerate_Enum(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"active", "inactive"}}}},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const StatusSchema = z.enum([\"active\", \"inactive\"]);\n" +
		"export type Status = z.infer<typeof StatusSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_ArrayField(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Pet",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "tags", Type: &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindRef, RefName: "Tag"}}}, Optional: true},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const PetSchema = z.object({ tags: z.array(TagSchema).optional(), });\n" +
		"export type Pet = z.infer<typeof PetSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Tag" {
		t.Errorf("expected Dependencies [Tag], got %v", outputs[0].Dependencies)
	}
}
```

- [ ] **Step 2: Run tests to check status**

Run: `go test ./internal/gen/zod/...`
Expected: These should already PASS, since Task 2 wrote the full `renderZodType` dispatch including `KindEnum` and `KindArray`. This step exists to confirm that — if either test fails, it reveals a bug in Task 2's `renderEnum`/`renderArray` that must be fixed before continuing (do not skip a failure here).

- [ ] **Step 3: Commit**

```bash
git add internal/gen/zod/generate_test.go
git commit -m "test: add enum and array coverage for Zod generator"
```

---

## Task 4: Zod generator — union, intersection, allOf-extends coverage

**Files:**
- Modify: `internal/gen/zod/generate_test.go`

**Interfaces:**
- Consumes: `renderUnion`, `renderIntersection`, `renderObject`'s `Extends` handling (already written in Task 2; test-only task).

- [ ] **Step 1: Write the failing tests**

Add to `internal/gen/zod/generate_test.go`:

```go
func TestGenerate_Union(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "StringOrNumber",
				Type: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{Variants: []*ir.Node{
					{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString},
					{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveNumber},
				}}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const StringOrNumberSchema = z.union([z.string(), z.number()]);\n" +
		"export type StringOrNumber = z.infer<typeof StringOrNumberSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_IntersectionFallback(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Weird",
				Type: &ir.Node{Kind: ir.KindIntersection, Intersection: &ir.IntersectionNode{Members: []*ir.Node{
					{Kind: ir.KindRef, RefName: "Base"},
					{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString},
				}}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const WeirdSchema = z.intersection(BaseSchema, z.string());\n" +
		"export type Weird = z.infer<typeof WeirdSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Base" {
		t.Errorf("expected Dependencies [Base], got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_ExtendsFromAllOf(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Dog",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Extends: []string{"Animal"},
						Fields: []*ir.Field{
							{Name: "breed", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const DogSchema = AnimalSchema.extend({ breed: z.string(), });\n" +
		"export type Dog = z.infer<typeof DogSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Animal" {
		t.Errorf("expected Dependencies [Animal], got %v", outputs[0].Dependencies)
	}
}
```

- [ ] **Step 2: Run tests to check status**

Run: `go test ./internal/gen/zod/...`
Expected: These should already PASS, since Task 2 wrote the full `renderObject`/`renderUnion`/`renderIntersection` logic. If any fail, fix the corresponding function in `internal/gen/zod/generate.go` before continuing.

- [ ] **Step 3: Commit**

```bash
git add internal/gen/zod/generate_test.go
git commit -m "test: add union, intersection, and allOf-extends coverage for Zod generator"
```

---

## Task 5: Zod generator — circular references

**Files:**
- Create: `internal/gen/zod/cyclic.go`
- Modify: `internal/gen/zod/generate.go` (add the `Cyclic` branch to `renderModel`)
- Test: `internal/gen/zod/cyclic_test.go`

**Interfaces:**
- Consumes: `ir.Model.Cyclic` (pre-existing field, now correctly populated for `allOf`-extends cycles too per Task 1), `ts.QuoteFieldName`.
- Produces: `renderCyclicModel(name string, node *ir.Node) (ModelOutput, error)`, `renderTSType(node *ir.Node) (string, []string)` and its helpers — a self-contained TS-type-expression renderer used only for a cyclic model's hand-written `export type X = ...` line. This intentionally duplicates (rather than imports) `internal/gen/ts`'s equivalent logic — see the design spec's "self-contained within the Zod generator's own output file" decision. `renderModel` (in `generate.go`) gets one new branch at the top checking `m.Cyclic`.

- [ ] **Step 1: Write the failing tests**

Create `internal/gen/zod/cyclic_test.go`:

```go
package zod_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/zod"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

func TestGenerate_SelfCycle(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name:   "Node",
				Cyclic: true,
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "value", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
							{Name: "next", Type: &ir.Node{Kind: ir.KindRef, RefName: "Node"}, Optional: true},
						},
					},
				},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Node = { value: string; next?: Node; };\n" +
		"export const NodeSchema: z.ZodType<Node> = z.lazy(() => z.object({ value: z.string(), next: NodeSchema.optional(), }));\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 0 {
		t.Errorf("expected no dependencies (self-ref removed), got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_MutualCycle(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "A", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "b", Type: &ir.Node{Kind: ir.KindRef, RefName: "B"}, Optional: true},
				}}},
			},
			{
				Name: "B", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "a", Type: &ir.Node{Kind: ir.KindRef, RefName: "A"}, Optional: true},
				}}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantA := "export type A = { b?: B; };\n" +
		"export const ASchema: z.ZodType<A> = z.lazy(() => z.object({ b: BSchema.optional(), }));\n"
	wantB := "export type B = { a?: A; };\n" +
		"export const BSchema: z.ZodType<B> = z.lazy(() => z.object({ a: ASchema.optional(), }));\n"
	if outputs[0].Declaration != wantA {
		t.Errorf("A got:\n%s\nwant:\n%s", outputs[0].Declaration, wantA)
	}
	if outputs[1].Declaration != wantB {
		t.Errorf("B got:\n%s\nwant:\n%s", outputs[1].Declaration, wantB)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "B" {
		t.Errorf("A deps: got %v, want [B]", outputs[0].Dependencies)
	}
	if len(outputs[1].Dependencies) != 1 || outputs[1].Dependencies[0] != "A" {
		t.Errorf("B deps: got %v, want [A]", outputs[1].Dependencies)
	}
}

func TestGenerate_AllOfExtendsCycle(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "A", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
					Extends: []string{"B"},
					Fields: []*ir.Field{
						{Name: "extra", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
					},
				}},
			},
			{
				Name: "B", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "a", Type: &ir.Node{Kind: ir.KindRef, RefName: "A"}, Optional: true},
				}}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantA := "export type A = B & { extra?: string; };\n" +
		"export const ASchema: z.ZodType<A> = z.lazy(() => BSchema.extend({ extra: z.string().optional(), }));\n"
	if outputs[0].Declaration != wantA {
		t.Errorf("A got:\n%s\nwant:\n%s", outputs[0].Declaration, wantA)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "B" {
		t.Errorf("A deps: got %v, want [B]", outputs[0].Dependencies)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gen/zod/...`
Expected: FAIL — all three cyclic models currently get the plain `z.infer` treatment (Task 2's `renderModel` doesn't check `Cyclic` yet), producing a self-referential `z.object` that doesn't match any of the `want` strings above (and would be broken Zod code if actually run — `z.object` can't reference a `const` that isn't finished being declared yet).

- [ ] **Step 3: Write the implementation**

In `internal/gen/zod/generate.go`, change `renderModel` to check `Cyclic` first:

```go
func (r *renderer) renderModel(m *ir.Model) (ModelOutput, error) {
	name := r.nameFor(m.Name)
	if m.Cyclic {
		return r.renderCyclicModel(name, m.Type)
	}
	expr, deps := r.renderZodType(m.Type)
	decl := fmt.Sprintf("export const %sSchema = %s;\nexport type %s = z.infer<typeof %sSchema>;\n", name, expr, name, name)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}, nil
}
```

Create `internal/gen/zod/cyclic.go`:

```go
package zod

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// renderCyclicModel renders a cyclic model as a hand-written TS type plus
// a z.lazy()-wrapped, explicitly z.ZodType<X>-annotated Zod schema — the
// standard Zod v3 pattern for self-referential schemas, where plain
// z.infer doesn't work. The TS type is rendered independently of the
// separate TS generator's output (see renderTSType below); this keeps the
// Zod generator's output file self-contained.
func (r *renderer) renderCyclicModel(name string, node *ir.Node) (ModelOutput, error) {
	tsExpr, tsDeps := r.renderTSType(node)
	zodExpr, zodDeps := r.renderZodType(node)
	deps := append(append([]string{}, tsDeps...), zodDeps...)
	decl := fmt.Sprintf(
		"export type %s = %s;\nexport const %sSchema: z.ZodType<%s> = z.lazy(() => %s);\n",
		name, tsExpr, name, name, zodExpr,
	)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}, nil
}

// renderTSType renders node as a bare TypeScript type expression. This
// mirrors internal/gen/ts's rendering rules (same combinator-wrapping,
// same field optional/nullable syntax, same wire-name-preserving field
// quoting via ts.QuoteFieldName) but is a separate, self-contained
// implementation — it exists only to give a cyclic model's z.ZodType<X>
// annotation something to point at, not to duplicate the TS generator's
// output files.
func (r *renderer) renderTSType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderTSPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := r.nameFor(node.RefName)
		return name, []string{name}
	case ir.KindEnum:
		return renderTSEnum(node.Enum), nil
	case ir.KindArray:
		return r.renderTSArray(node.Array)
	case ir.KindUnion:
		return r.renderTSVariantList(node.Union.Variants, " | ")
	case ir.KindIntersection:
		return r.renderTSVariantList(node.Intersection.Members, " & ")
	case ir.KindObject:
		return r.renderTSObject(node.Object)
	default:
		return "unknown", nil
	}
}

func renderTSPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "string"
	case ir.PrimitiveNumber, ir.PrimitiveInteger:
		return "number"
	case ir.PrimitiveBoolean:
		return "boolean"
	default:
		return "unknown"
	}
}

func renderTSEnum(e *ir.EnumNode) string {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		parts[i] = strconv.Quote(v)
	}
	return strings.Join(parts, " | ")
}

func (r *renderer) renderTSArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := r.renderTSType(a.Items)
	return wrapForTSCombinator(itemExpr, a.Items) + "[]", deps
}

func (r *renderer) renderTSVariantList(nodes []*ir.Node, sep string) (string, []string) {
	parts := make([]string, len(nodes))
	var deps []string
	for i, n := range nodes {
		expr, d := r.renderTSType(n)
		parts[i] = wrapForTSCombinator(expr, n)
		deps = append(deps, d...)
	}
	return strings.Join(parts, sep), deps
}

func (r *renderer) renderTSObject(o *ir.ObjectNode) (string, []string) {
	var deps []string
	var parts []string
	for _, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		parts = append(parts, name)
	}
	if len(parts) == 0 || len(o.Fields) > 0 {
		fieldsExpr, fieldDeps := r.renderTSFields(o.Fields)
		deps = append(deps, fieldDeps...)
		parts = append(parts, fieldsExpr)
	}
	return strings.Join(parts, " & "), deps
}

func (r *renderer) renderTSFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := r.renderTSType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForTSCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("%s%s: %s;", ts.QuoteFieldName(f.Name), optional, typeExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

func wrapForTSCombinator(expr string, n *ir.Node) string {
	switch n.Kind {
	case ir.KindUnion, ir.KindIntersection:
		return "(" + expr + ")"
	case ir.KindObject:
		if n.Object != nil && len(n.Object.Extends) > 0 {
			return "(" + expr + ")"
		}
	}
	return expr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gen/zod/...`
Expected: PASS

Run: `go test ./...`
Expected: PASS (confirms Tasks 2-4's non-cyclic tests are unaffected by the new `Cyclic` branch)

- [ ] **Step 5: Commit**

```bash
git add internal/gen/zod/generate.go internal/gen/zod/cyclic.go internal/gen/zod/cyclic_test.go
git commit -m "feat: render cyclic models with z.lazy()/z.ZodType<X> and a hand-written TS type"
```

---

## Task 6: pkg/engine — GenerateZod orchestration (monolithic output)

**Files:**
- Modify: `pkg/engine/engine.go` (add `ZodOptions`)
- Modify: `pkg/engine/output.go` (add `GenerateZod`, `monolithicZodOutput`)
- Test: `pkg/engine/engine_test.go`

**Interfaces:**
- Consumes: `zod.Generate`, `zod.ModelOutput` (Task 2), `engine.Output` (pre-existing).
- Produces: `engine.ZodOptions{Modular bool}`, `engine.GenerateZod(doc *ir.Document, opts ZodOptions) (Output, error)`. Mirrors `GenerateTS`'s structure exactly, including deliberately ignoring `opts.Modular` for now (always monolithic) — Task 7 adds the modular branch, exactly the same staged split `GenerateTS` itself went through (Tasks 12/13 of the core-engine plan), to avoid forward-referencing Task 7's `modularZodOutput` before it exists.

- [ ] **Step 1: Write the failing test**

Add to `pkg/engine/engine_test.go` (reusing the existing `simpleSpec` constant already defined in that file for the TS monolithic test — do not redeclare it):

```go
func TestGenerateZod_Monolithic(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	content, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in output")
	}
	if !strings.Contains(content, `import { z } from "zod";`) {
		t.Errorf("expected zod import, got:\n%s", content)
	}
	if !strings.Contains(content, "export const PetSchema = z.object({") {
		t.Errorf("expected PetSchema, got:\n%s", content)
	}
	if len(out.Files) != 1 {
		t.Errorf("expected exactly 1 file for monolithic output, got %d", len(out.Files))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/engine/...`
Expected: FAIL — `engine.GenerateZod`/`engine.ZodOptions` undefined.

- [ ] **Step 3: Write the implementation**

In `pkg/engine/engine.go`, add next to the existing `TSOptions`:

```go
// ZodOptions controls Zod output shape.
type ZodOptions struct {
	// Modular selects one-file-per-model output (with a barrel index.ts)
	// instead of a single monolithic file.
	Modular bool
}
```

In `pkg/engine/output.go`, add the import for `internal/gen/zod` and the new function:

```go
import (
	"fmt"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/gen/zod"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)
```

```go
// GenerateZod renders doc as Zod v3 schemas, laid out per opts.
//
// Note: opts.Modular is not yet consulted — Task 7 adds the modular
// branch alongside modularZodOutput. Until then this always produces
// monolithic output, which is all this task's own test exercises.
func GenerateZod(doc *ir.Document, opts ZodOptions) (Output, error) {
	models, err := zod.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	return monolithicZodOutput(models), nil
}

func monolithicZodOutput(models []zod.ModelOutput) Output {
	var sb strings.Builder
	sb.WriteString("import { z } from \"zod\";\n\n")
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"index.ts": sb.String()}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/engine/engine.go pkg/engine/output.go pkg/engine/engine_test.go
git commit -m "feat: add pkg/engine.GenerateZod with monolithic output"
```

---

## Task 7: pkg/engine — modular Zod output mode

**Files:**
- Modify: `pkg/engine/output.go`
- Modify: `pkg/engine/engine_test.go`

**Interfaces:**
- Consumes: `zod.ModelOutput{Name, Declaration, Dependencies}` (Task 2), `Output`, `GenerateZod` (Task 6).
- Produces: `modularZodOutput`, wired into `GenerateZod` (which this task also modifies to branch on `opts.Modular`) — same staged pattern as `GenerateTS`'s Task 12→13 split.

- [ ] **Step 1: Write the failing test**

Add to `pkg/engine/engine_test.go` (reusing the existing `twoModelSpec` constant already defined in that file for the TS modular test — do not redeclare it):

```go
func TestGenerateZod_Modular(t *testing.T) {
	doc, err := engine.Parse([]byte(twoModelSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateZod: %v", err)
	}
	if _, ok := out.Files["Category.ts"]; !ok {
		t.Error("expected Category.ts")
	}
	petFile, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatal("expected Pet.ts")
	}
	if !strings.Contains(petFile, `import { z } from "zod";`) {
		t.Errorf("expected zod import in Pet.ts, got:\n%s", petFile)
	}
	if !strings.Contains(petFile, `import { CategorySchema, type Category } from "./Category";`) {
		t.Errorf("expected dependency import in Pet.ts, got:\n%s", petFile)
	}
	barrel, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected barrel index.ts")
	}
	if !strings.Contains(barrel, `export * from "./Category";`) || !strings.Contains(barrel, `export * from "./Pet";`) {
		t.Errorf("expected barrel exports for both models, got:\n%s", barrel)
	}
	if len(out.Files) != 3 {
		t.Errorf("expected 3 files (Category.ts, Pet.ts, index.ts), got %d: %v", len(out.Files), out.Files)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/engine/...`
Expected: FAIL — `GenerateZod` currently always calls `monolithicZodOutput` regardless of `opts.Modular`, so `Category.ts`/`Pet.ts` are absent from `out.Files`.

- [ ] **Step 3: Write the implementation**

In `pkg/engine/output.go`, update `GenerateZod` to branch on `opts.Modular`, and add `modularZodOutput`:

```go
// GenerateZod renders doc as Zod v3 schemas, laid out per opts.
func GenerateZod(doc *ir.Document, opts ZodOptions) (Output, error) {
	models, err := zod.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularZodOutput(models), nil
	}
	return monolithicZodOutput(models), nil
}
```

```go
// modularZodOutput writes one file per model plus a barrel index.ts. Each
// model file imports both the schema constant and the plain type from
// every dependency (import { XSchema, type X } from "./X";) — a cyclic
// model's hand-written TS type references the plain type name while its
// Zod body references the schema constant, so importing both uniformly
// avoids tracking which form a given dependent needs.
func modularZodOutput(models []zod.ModelOutput) Output {
	files := make(map[string]string, len(models)+1)
	var barrel strings.Builder
	for _, m := range models {
		fileName := m.Name + ".ts"
		var content strings.Builder
		content.WriteString("import { z } from \"zod\";\n")
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import { %sSchema, type %s } from \"./%s\";\n", dep, dep, dep))
		}
		content.WriteString("\n")
		content.WriteString(m.Declaration)
		files[fileName] = content.String()
		barrel.WriteString(fmt.Sprintf("export * from \"./%s\";\n", m.Name))
	}
	files["index.ts"] = barrel.String()
	return Output{Files: files}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/engine/output.go pkg/engine/engine_test.go
git commit -m "feat: add modular Zod output mode with per-model files and barrel index.ts"
```

---

## Task 8: CLI wiring — `--target zod`

**Files:**
- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`
- Modify: `internal/cli/parse_test.go`

**Interfaces:**
- Consumes: `engine.GenerateZod`, `engine.ZodOptions` (Task 6/7).
- Produces: `parseArgs` now accepts `--target zod` in addition to `--target ts`; `Run` dispatches to `GenerateZod` when `opts.Target == "zod"`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/parse_test.go`:

```go
func TestParseArgs_TargetZod(t *testing.T) {
	opts, err := parseArgs([]string{"pull", "spec.json", "--target", "zod", "--output", "./out"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.Target != "zod" {
		t.Errorf("expected Target zod, got %q", opts.Target)
	}
}
```

Add to `internal/cli/run_test.go` (reusing the existing `fixtureSpec` constant already defined in that file — do not redeclare it):

```go
func TestRun_EndToEndZod(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(dir, "out.ts")

	var stderr bytes.Buffer
	code := Run([]string{"pull", specPath, "--target", "zod", "--out-file", outPath}, strings.NewReader(""), &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "export const PetSchema = z.object({") {
		t.Errorf("expected PetSchema, got:\n%s", content)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `parseArgs` currently rejects `--target zod` with "not yet implemented"; `Run` currently always calls `GenerateTS` regardless of `opts.Target`.

- [ ] **Step 3: Write the implementation**

In `internal/cli/parse.go`, update the target flag's help text and validation:

```go
	target := fs.String("target", "ts", `generation target: "ts" or "zod"`)
```

```go
	if *target != "ts" && *target != "zod" {
		return Options{}, fmt.Errorf("target %q not yet implemented (only \"ts\" and \"zod\" are currently supported)", *target)
	}
```

In `internal/cli/run.go`, replace the single `engine.GenerateTS` call with a dispatch on `opts.Target`:

```go
	var out engine.Output
	if opts.Target == "zod" {
		out, err = engine.GenerateZod(doc, engine.ZodOptions{Modular: opts.OutDir != ""})
	} else {
		out, err = engine.GenerateTS(doc, engine.TSOptions{Modular: opts.OutDir != ""})
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
```

(This replaces the existing `out, err := engine.GenerateTS(...)` block and its immediately-following `if err != nil` check — the rest of `Run`, including the final `writeOutput` call, is unchanged.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/...`
Expected: PASS

Run: `go test ./...`
Expected: PASS (confirms the existing `--target ts` path, and `cmd/openapi2code`'s end-to-end test from the CLI plan, are both unaffected)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/parse.go internal/cli/run.go internal/cli/run_test.go internal/cli/parse_test.go
git commit -m "feat: wire --target zod through the CLI"
```

---

## Task 9: Golden-file coverage for Zod output

**Files:**
- Create: `pkg/engine/testdata/allof-extends-cycle.yaml`
- Modify: `pkg/engine/golden_test.go`
- Create (generated by Step 4, committed in Step 6): `pkg/engine/testdata/golden/petstore.zod.ts`, `pkg/engine/testdata/golden/composition.zod.ts`, `pkg/engine/testdata/golden/nullable_enum.zod.ts`, `pkg/engine/testdata/golden/allof-extends-cycle.zod.ts`

**Interfaces:**
- Consumes: `engine.Parse`, `engine.GenerateZod`, `engine.ZodOptions` (Tasks 6-7).

- [ ] **Step 1: Write the new fixture spec**

Create `pkg/engine/testdata/allof-extends-cycle.yaml` — exercises the Task 1 cycle-detection fix (a cycle formed purely through `allOf` inheritance, not through a plain object field ref) with a shape nothing else in the existing golden suite covers:

```yaml
openapi: "3.0.3"
info:
  title: AllOf-extends cycle fixture
  version: "1.0.0"
components:
  schemas:
    A:
      allOf:
        - $ref: "#/components/schemas/B"
        - type: object
          properties:
            extra:
              type: string
    B:
      type: object
      properties:
        a:
          $ref: "#/components/schemas/A"
```

- [ ] **Step 2: Extend the golden test harness**

In `pkg/engine/golden_test.go`, add a second test function alongside the existing `TestGoldenFixtures` (which continues to test TS output unchanged — do not modify it), reusing the same fixture files already used there for `petstore`, `composition`, and `nullable_enum`, plus the new `allof-extends-cycle` fixture. Golden files use a `.zod.ts` suffix so they sit next to (and are clearly distinct from) the existing `.ts` TS golden files in the same `testdata/golden/` directory:

```go
func TestGoldenFixtures_Zod(t *testing.T) {
	fixtures := []string{"petstore", "composition", "nullable_enum", "allof-extends-cycle"}
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			doc, err := engine.Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := engine.GenerateZod(doc, engine.ZodOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateZod: %v", err)
			}
			got, ok := out.Files["index.ts"]
			if !ok {
				t.Fatal("monolithic output has no index.ts")
			}
			goldenPath := filepath.Join("testdata", "golden", name+".zod.ts")
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
					t.Fatalf("mkdir golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
				t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}
```

(This reuses the package-level `update` flag already declared in `golden_test.go` for the TS golden suite — do not redeclare it.)

- [ ] **Step 3: Confirm the test fails without golden files**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures_Zod`
Expected: FAIL — golden files don't exist yet.

- [ ] **Step 4: Generate the golden files**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures_Zod -update`
Expected: PASS (this run only writes files, it doesn't compare).

- [ ] **Step 5: Manually verify the generated golden output**

Open each generated file under `pkg/engine/testdata/golden/` and check it against the expected shape below (hand-derived by tracing the exact rendering logic from Tasks 2-5). If anything differs in a way that's a genuine generator bug (not just incidental whitespace), fix the relevant function in `internal/gen/zod/generate.go` or `cyclic.go`, then re-run Step 4 to regenerate.

`petstore.zod.ts` — 4 declarations in name-sorted order (Category, Error, Pet, Tag), each field-sorted alphabetically, none cyclic:

```
import { z } from "zod";

export const CategorySchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Category = z.infer<typeof CategorySchema>;

export const ErrorSchema = z.object({ code: z.number(), message: z.string(), });
export type Error = z.infer<typeof ErrorSchema>;

export const PetSchema = z.object({ category: CategorySchema.optional(), id: z.number().optional(), name: z.string(), photoUrls: z.array(z.string()), status: z.enum(["available", "pending", "sold"]).optional(), tags: z.array(TagSchema).optional(), });
export type Pet = z.infer<typeof PetSchema>;

export const TagSchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Tag = z.infer<typeof TagSchema>;
```

`composition.zod.ts` — 4 declarations, name-sorted (Animal, Dog, Pet, StringOrNumber):

```
import { z } from "zod";

export const AnimalSchema = z.object({ name: z.string(), });
export type Animal = z.infer<typeof AnimalSchema>;

export const DogSchema = AnimalSchema.extend({ breed: z.string(), });
export type Dog = z.infer<typeof DogSchema>;

export const PetSchema = z.union([DogSchema, AnimalSchema]);
export type Pet = z.infer<typeof PetSchema>;

export const StringOrNumberSchema = z.union([z.string(), z.number()]);
export type StringOrNumber = z.infer<typeof StringOrNumberSchema>;
```

`nullable_enum.zod.ts` — 2 declarations, name-sorted (Account, Status); note `nickname` is optional+nullable (`.nullable().optional()`), `note` is required+nullable (`.nullable()` only):

```
import { z } from "zod";

export const AccountSchema = z.object({ id: z.string(), nickname: z.string().nullable().optional(), note: z.string().nullable(), status: StatusSchema, });
export type Account = z.infer<typeof AccountSchema>;

export const StatusSchema = z.enum(["active", "inactive", "pending"]);
export type Status = z.infer<typeof StatusSchema>;
```

`allof-extends-cycle.zod.ts` — 2 declarations, name-sorted (A, B), BOTH cyclic (this is what Task 1's fix makes correct — without it, both would incorrectly get the plain `z.infer` treatment and the generated code would be broken).

**Updated during execution:** the checklist below reflects Task 5's fix round (commit 3d06d3b), not Task 5's original pre-fix behavior. Task 5's review found that annotating a cyclic model's schema as `z.ZodType<X>` breaks `.extend()`/`.shape` (only defined on `ZodObject`, not the wider `ZodType`) whenever an `Extends` target is itself cyclic — verified by actually compiling with `tsc`. The fix: when any `Extends` target is cyclic, `renderObject` folds the extends targets and own-fields as a `z.intersection(...)` instead of chaining `.extend()`. A's `Extends: ["B"]` target (B) is cyclic here, so A's Zod body now uses `z.intersection`, not `.extend()`. B has no `Extends` at all, so B's rendering is unaffected by the fix (identical to the original pre-fix derivation):

```
import { z } from "zod";

export type A = B & { extra?: string; };
export const ASchema: z.ZodType<A> = z.lazy(() => z.intersection(BSchema, z.object({ extra: z.string().optional(), })));

export type B = { a?: A; };
export const BSchema: z.ZodType<B> = z.lazy(() => z.object({ a: ASchema.optional(), }));
```

- [ ] **Step 6: Run the golden test suite for real**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures_Zod`
Expected: PASS

- [ ] **Step 7: Run the full test suite**

Run: `go build ./...`
Run: `go vet ./...`
Run: `go test ./...`
Expected: all PASS / clean, including the pre-existing `TestGoldenFixtures` (TS) suite, unchanged

- [ ] **Step 8: Commit**

```bash
git add pkg/engine/testdata/allof-extends-cycle.yaml pkg/engine/testdata/golden pkg/engine/golden_test.go
git commit -m "test: add golden-file coverage for Zod output, including an allOf-extends cycle"
```

---

## Final verification

- [ ] Run `go build ./...` — expect success with no errors.
- [ ] Run `go vet ./...` — expect no warnings.
- [ ] Run `go test ./...` — expect all packages PASS, including every previously-committed golden file (TS) unchanged.
- [ ] Manually run the built binary once against a real spec to sanity-check the UX: `go run ./cmd/openapi2code pull <some-spec> --target zod --output /tmp/out` and inspect `/tmp/out` — if `zod` is installed in a scratch npm project, optionally `tsc --noEmit` the output to confirm it actually type-checks (not required for automated tests, but a valuable manual sanity check given generated code is never executed by this repo's own test suite).
