# Swift, Kotlin, and Dart Generators Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Swift, Kotlin, and Dart native mobile model generators, completing the PRD's full target list, following the exact architectural pattern the TypeScript and Zod generators already established.

**Architecture:** Three new `internal/gen/<lang>` packages consume the existing `ir.Document` with no IR changes; each gets a `Generate<Lang>`/`<Lang>Options` pair in `pkg/engine` and a new `--target` value in `internal/cli`. A small shared `internal/gen/mobile` package holds naming helpers all three languages need (JSON field names becoming idiomatic camelCase, enum values becoming idiomatic per-language casing).

**Tech Stack:** Go 1.25 (stdlib only) for the generators. Verification uses each language's real compiler where available in this environment (`swiftc` is installed; `kotlinc`/`dart` are not, so their equivalent checks are opt-in and skip cleanly).

**Spec:** `docs/superpowers/specs/2026-09-11-mobile-generators-design.md` (including its three addenda on inline enums/objects and the complete node-kind handling rule set — read the whole file, not just the top section)

## Global Constraints

- **Nullability folds to one axis.** A field is `T?` (Swift/Kotlin/Dart's only optionality mechanism) if the IR's `Field.Optional` **or** `Field.Nullable` is true; plain `T` otherwise.
- **Field names become camelCase with an explicit wire-name mapping** — a `CodingKeys` enum in Swift, an explicit key string in Kotlin/Dart's hand-written (de)serialization.
- **Cyclic models (`ir.Model.Cyclic`) are Swift `class`; everything else is Swift `struct`.** Kotlin and Dart never need this — their objects are always references, so no special-casing.
- **`allOf` always flattens** into one combined field list; no inheritance is ever modeled in generated Swift/Kotlin/Dart code, even where the source schema conceptually reads as "extends" (none of the three languages has an inheritance mechanism that maps onto multi-member `allOf`).
- **Enums are string-backed with an explicit value round-trip:** Swift `enum X: String, Codable`; Kotlin `enum class X(val value: String)`; Dart enhanced enum with a `final String value` field.
- **oneOf/anyOf, allOf-with-non-object-member (`ir.KindIntersection`), and `ir.PrimitiveUnknown` are all unsupported** — skipped with a comment, at the model level (whole model omitted, generation continues) or field level (that field omitted, generation continues for the model's other fields).
- **An inline (non-`$ref`) `ir.KindEnum` or `ir.KindObject` reached directly from a field gets a synthesized top-level name**, `<ModelName><FieldNamePascalCase>` (e.g. `PetStatus`), recursively for nested cases.
- **JSON approach:** Swift uses `Codable` (no generated boilerplate beyond `CodingKeys`). Kotlin and Dart get hand-written `toJson()`/`fromJson(...)` methods using each language's standard-library `Map` type — no dependency, no annotation, no build-step requirement on the consumer.
- **Both output modes** (modular: one file per model; monolithic: single file via `--out-file`) are supported for all three, mirroring `TSOptions{Modular}`/`ZodOptions{Modular}`.
- **No barrel/index file for any of the three mobile languages in modular mode** — a deliberate deviation from TS/Zod's `index.ts` barrel. Swift files in one target, and Kotlin files in one package (none of the generated files declare a package, so they share the default package), see every other type automatically with no import and no re-export mechanism; a barrel serves no purpose. Dart needs explicit imports between files (unlike Swift/Kotlin), but nothing in Dart's own tooling expects or benefits from an index-style re-export file the way JS bundlers do, so modular Dart output is likewise just one file per model, no barrel.
- **No package/namespace declaration is emitted by default** in Kotlin or Dart (Kotlin compiles into the root package with no statement; Dart needs none). This is a scope decision, not a limitation to work around — do not add a `--package` flag.
- **`--target` accepts `swift`, `kotlin`, and `dart` as three more single values**, exactly like `ts`/`zod` today — comma-separated multi-target stays out of scope.

---

### Task 1: Shared naming helpers (`internal/gen/mobile`)

**Files:**
- Create: `internal/gen/mobile/camelcase.go`
- Test: `internal/gen/mobile/camelcase_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `mobile.ToCamelCase(name string) string`, `mobile.ToPascalCase(name string) string`, `mobile.ToScreamingSnakeCase(name string) string` — used by all three language packages in later tasks for field naming, synthesized-type naming, and Kotlin enum-constant naming respectively.

- [ ] **Step 1: Write the failing tests**

Create `internal/gen/mobile/camelcase_test.go`:

```go
package mobile_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

func TestToCamelCase(t *testing.T) {
	cases := map[string]string{
		"photo_urls":   "photoUrls",
		"photoUrls":    "photoUrls",
		"PhotoUrls":    "photoUrls",
		"name":         "name",
		"in-progress":  "inProgress",
		"already done": "alreadyDone",
		"":             "",
	}
	for input, want := range cases {
		if got := mobile.ToCamelCase(input); got != want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	cases := map[string]string{
		"status":     "Status",
		"photo_urls": "PhotoUrls",
		"":           "",
	}
	for input, want := range cases {
		if got := mobile.ToPascalCase(input); got != want {
			t.Errorf("ToPascalCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToScreamingSnakeCase(t *testing.T) {
	cases := map[string]string{
		"available":   "AVAILABLE",
		"in_progress": "IN_PROGRESS",
		"inProgress":  "IN_PROGRESS",
		"in-progress": "IN_PROGRESS",
	}
	for input, want := range cases {
		if got := mobile.ToScreamingSnakeCase(input); got != want {
			t.Errorf("ToScreamingSnakeCase(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gen/mobile/... -v`
Expected: FAIL — the `mobile` package doesn't exist yet, so this fails to compile.

- [ ] **Step 3: Write the implementation**

Create `internal/gen/mobile/camelcase.go`:

```go
// Package mobile holds naming helpers shared by the Swift, Kotlin, and
// Dart generators: converting a JSON field or enum-value name into the
// idiomatic casing each language's conventions expect.
package mobile

import "strings"

// ToCamelCase converts a JSON field or enum-value name (commonly
// snake_case or kebab-case in real-world APIs) into idiomatic
// lowerCamelCase, as Swift, Kotlin, and Dart properties and Dart/Swift
// enum cases all use. A name with no separators passes through with only
// its first rune lowercased, so an already-camelCase or PascalCase input
// is handled correctly too.
func ToCamelCase(name string) string {
	parts := splitWords(name)
	if len(parts) == 0 {
		return name
	}
	var sb strings.Builder
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		if i == 0 {
			sb.WriteString(strings.ToLower(string(runes[0])))
			sb.WriteString(string(runes[1:]))
		} else {
			sb.WriteString(strings.ToUpper(string(runes[0])))
			sb.WriteString(strings.ToLower(string(runes[1:])))
		}
	}
	return sb.String()
}

// ToPascalCase converts a JSON field or model name into idiomatic
// UpperCamelCase, used for synthesized type names (combining a model
// name with a field name to name an inline enum or object).
func ToPascalCase(name string) string {
	camel := ToCamelCase(name)
	if camel == "" {
		return camel
	}
	runes := []rune(camel)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// ToScreamingSnakeCase converts a JSON enum value into idiomatic
// SCREAMING_SNAKE_CASE, as Kotlin enum constants conventionally use.
func ToScreamingSnakeCase(name string) string {
	var sb strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '-' || r == ' ' || r == '_':
			sb.WriteRune('_')
		case r >= 'A' && r <= 'Z' && i > 0 && isLowerOrDigit(runes[i-1]):
			sb.WriteRune('_')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return strings.ToUpper(sb.String())
}

func isLowerOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// splitWords breaks name on underscores, hyphens, and spaces into its
// constituent words, discarding empty segments from consecutive
// separators.
func splitWords(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gen/mobile/... -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gen/mobile/camelcase.go internal/gen/mobile/camelcase_test.go
git commit -m "$(cat <<'EOF'
Add shared naming helpers for the mobile generators

ToCamelCase/ToPascalCase/ToScreamingSnakeCase convert JSON field and
enum-value names into the casing Swift/Kotlin/Dart conventions
expect. Shared by all three upcoming generator packages.

EOF
)"
```

---

### Task 2: Swift generator, engine wiring, CLI wiring, and golden tests

**Files:**
- Create: `internal/gen/swift/identifier.go`
- Create: `internal/gen/swift/generate.go`
- Create: `internal/gen/swift/generate_test.go`
- Modify: `pkg/engine/output.go` (add `GenerateSwift`/`SwiftOptions`)
- Modify: `internal/cli/parse.go` (accept `swift` as a `--target` value)
- Modify: `internal/cli/run.go` (dispatch `swift` to `engine.GenerateSwift`)
- Create: `pkg/engine/testdata/golden/petstore.swift.txt`, `pkg/engine/testdata/golden/circular.swift.txt`, `pkg/engine/testdata/golden/composition.swift.txt`, `pkg/engine/testdata/golden/nullable_enum.swift.txt` (generated via `-update`, see Step 6)
- Modify: `pkg/engine/golden_test.go` (add Swift cases to the existing golden-fixture table)

**Interfaces:**
- Consumes: `ir.Document`/`ir.Model`/`ir.Node` (unchanged, from `internal/ir`); `mobile.ToCamelCase`/`ToPascalCase` (Task 1).
- Produces: `swift.ModelOutput{Name string, Declaration string}`, `swift.Generate(doc *ir.Document) ([]swift.ModelOutput, error)`; `engine.GenerateSwift(doc *ir.Document, opts engine.SwiftOptions) (engine.Output, error)`, `engine.SwiftOptions{Modular bool}`. Later tasks (Task 3) read `swift.ModelOutput` and the golden fixtures this task creates.

This is the largest task in the plan — read all of it before starting, and read `docs/superpowers/specs/2026-09-11-mobile-generators-design.md` in full (including its addenda) alongside the Global Constraints above before writing any code.

- [ ] **Step 1: Write `internal/gen/swift/identifier.go`**

```go
// Package swift generates Swift Codable structs/classes from the IR.
package swift

import (
	"regexp"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Swift keywords that cannot be used as a bare
// identifier (a subset commonly hit by real-world OpenAPI schema/field
// names; Swift technically allows any keyword as an identifier via
// backticks, but suffixing is simpler and matches this project's existing
// TS/Zod precedent).
var reservedWords = map[string]bool{
	"associatedtype": true, "class": true, "deinit": true, "enum": true,
	"extension": true, "fileprivate": true, "func": true, "import": true,
	"init": true, "inout": true, "internal": true, "let": true,
	"open": true, "operator": true, "private": true, "protocol": true,
	"public": true, "rethrows": true, "static": true, "struct": true,
	"subscript": true, "typealias": true, "var": true, "break": true,
	"case": true, "continue": true, "default": true, "defer": true,
	"do": true, "else": true, "fallthrough": true, "for": true,
	"guard": true, "if": true, "in": true, "repeat": true, "return": true,
	"switch": true, "where": true, "while": true, "as": true, "Any": true,
	"catch": true, "false": true, "is": true, "nil": true, "self": true,
	"Self": true, "super": true, "throw": true, "throws": true,
	"true": true, "try": true,
}

func sanitize(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	if reservedWords[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// SanitizeTypeIdentifier converts a model or synthesized type name into a
// valid Swift type identifier. Casing is left as-is: OpenAPI schema names
// and this package's own synthesized names (see resolveType) are already
// PascalCase.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Swift property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// SanitizeEnumCaseIdentifier converts a JSON enum value into a valid,
// idiomatic Swift enum case identifier.
func SanitizeEnumCaseIdentifier(value string) string {
	return sanitize(mobile.ToCamelCase(value))
}
```

- [ ] **Step 2: Write `internal/gen/swift/generate.go`**

```go
package swift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Swift declaration: a struct, a
// class (for cyclic models), an enum, or a typealias.
type ModelOutput struct {
	Name        string
	Declaration string
}

// extraDecl is a top-level declaration synthesized while resolving a
// field's type — an inline (non-$ref) enum or object needs a real
// top-level name in Swift, since Swift has no anonymous type literal for
// either shape. See the design spec's "Addendum: inline (field-level)
// enums" and its generalization to inline objects.
type extraDecl struct {
	name string
	node *ir.Node
}

// Generate renders every model in doc as a Swift declaration, plus any
// declarations synthesized along the way for inline enums/objects. Order
// is deterministic: doc.Models first (name-sorted, as ir.Build produces
// them), then synthesized declarations in the order they were
// discovered.
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	names := assignNames(doc.Models)
	used := make(map[string]bool, len(doc.Models))
	for _, n := range names {
		used[n] = true
	}
	r := &renderer{names: names}

	type queueItem struct {
		name string
		node *ir.Node
	}
	queue := make([]queueItem, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, queueItem{name: names[m.Name], node: modelNode(m)})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, extra, err := r.renderDeclaration(item.name, item.node, isCyclicRoot(doc, item.name, names))
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.name, err)
		}
		if decl == "" {
			continue // skipped: union/intersection/unknown as this type's own shape
		}
		outputs = append(outputs, ModelOutput{Name: item.name, Declaration: decl})
		for _, e := range extra {
			name := uniquify(SanitizeTypeIdentifier(e.name), used)
			used[name] = true
			queue = append(queue, queueItem{name: name, node: e.node})
		}
	}
	return outputs, nil
}

// modelNode returns m's own type node — a small indirection so Generate's
// queue can carry ir.Model-derived and synthesized items uniformly.
func modelNode(m *ir.Model) *ir.Node {
	return m.Type
}

// isCyclicRoot reports whether name (already resolved to its Swift
// identifier) belongs to a model flagged Cyclic in doc — only a
// document's own top-level models can be cyclic (a synthesized inline
// enum/object can never participate in a $ref cycle, since nothing can
// $ref an anonymous type), so this only needs to check doc.Models.
func isCyclicRoot(doc *ir.Document, name string, names map[string]string) bool {
	for _, m := range doc.Models {
		if names[m.Name] == name {
			return m.Cyclic
		}
	}
	return false
}

// assignNames maps each model's IR name to its Swift type identifier,
// mirroring internal/gen/ts's assignNames: SanitizeTypeIdentifier, then a
// _2/_3 suffix on collision, in doc.Models order. Swift has no reserved
// barrel name (see Global Constraints — no barrel file), so nothing is
// pre-reserved the way ts.assignNames reserves "index".
func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{}
	names := make(map[string]string, len(models))
	for _, m := range models {
		name := uniquify(SanitizeTypeIdentifier(m.Name), used)
		used[name] = true
		names[m.Name] = name
	}
	return names
}

func uniquify(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
}

type renderer struct {
	names map[string]string
}

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

// renderDeclaration renders node as a top-level Swift declaration named
// name. cyclic is only meaningful when node.Kind == ir.KindObject (an
// enum or typealias is never cyclic; a synthesized inline object is never
// the target of a $ref, so it is never cyclic either — cyclic applies
// only to a document's own top-level object models).
func (r *renderer) renderDeclaration(name string, node *ir.Node, cyclic bool) (string, []extraDecl, error) {
	switch node.Kind {
	case ir.KindObject:
		return r.renderObjectDecl(name, node.Object, cyclic)
	case ir.KindEnum:
		return r.renderEnumDecl(name, node.Enum), nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return "", nil, nil // skipped, per the design's unsupported-shape rule
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, renderPrimitive(node.Primitive)), nil, nil
	case ir.KindArray:
		expr, skip, extra := r.resolveType(node, name)
		if skip {
			return "", nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, expr), extra, nil
	case ir.KindRef:
		return fmt.Sprintf("public typealias %s = %s\n", name, r.nameFor(node.RefName)), nil, nil
	default:
		return "", nil, nil
	}
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "String"
	case ir.PrimitiveNumber:
		return "Double"
	case ir.PrimitiveInteger:
		return "Int"
	case ir.PrimitiveBoolean:
		return "Bool"
	default:
		return "String"
	}
}

func (r *renderer) renderEnumDecl(name string, e *ir.EnumNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "public enum %s: String, Codable {\n", name)
	for _, v := range e.Values {
		caseName := SanitizeEnumCaseIdentifier(v)
		if caseName == v {
			fmt.Fprintf(&sb, "    case %s\n", caseName)
		} else {
			fmt.Fprintf(&sb, "    case %s = %s\n", caseName, strconv.Quote(v))
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// resolvedField is one field ready to render into a struct/class body.
type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	kind         ir.Kind // the field's own node kind, needed by decodeExpr/encodeExpr below
	isEnumRef    bool    // true if kind == KindRef and the target is an enum (needed for nothing in Swift — Codable handles it uniformly — kept for symmetry with Kotlin/Dart's renderer shape)
	optional     bool
}

// resolveFields resolves every field of an object into resolvedFields,
// synthesizing a top-level name for any inline enum/object it encounters.
// containingTypeName seeds the synthesized name (<ModelName><FieldName>).
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) ([]resolvedField, []extraDecl) {
	var resolved []resolvedField
	var allExtra []extraDecl
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		expr, skip, extra := r.resolveType(f.Type, suggested)
		if skip {
			continue
		}
		optional := f.Optional || f.Nullable
		if optional {
			expr += "?"
		}
		resolved = append(resolved, resolvedField{
			propertyName: SanitizeFieldIdentifier(f.Name),
			wireName:     f.Name,
			typeExpr:     expr,
			kind:         f.Type.Kind,
			optional:     optional,
		})
		allExtra = append(allExtra, extra...)
	}
	return resolved, allExtra
}

// resolveType resolves node to a non-optional Swift type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object. skip is true when node has no
// representable Swift type (a union, an allOf-with-non-object-member
// intersection, or an unrecognized primitive) — the caller omits
// whatever position this type would have occupied.
func (r *renderer) resolveType(node *ir.Node, suggestedName string) (expr string, skip bool, extra []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", true, nil
		}
		return renderPrimitive(node.Primitive), false, nil
	case ir.KindRef:
		return r.nameFor(node.RefName), false, nil
	case ir.KindArray:
		itemExpr, itemSkip, itemExtra := r.resolveType(node.Array.Items, suggestedName)
		if itemSkip {
			return "", true, nil
		}
		return "[" + itemExpr + "]", false, itemExtra
	case ir.KindEnum, ir.KindObject:
		return suggestedName, false, []extraDecl{{name: suggestedName, node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return "", true, nil
	default:
		return "", true, nil
	}
}

// renderObjectDecl renders o (already flattened — see Global Constraints,
// allOf always flattens) as a struct (or, when cyclic, a class with a
// hand-written init(from:) — Codable still synthesizes encode(to:) for a
// class with a custom init(from:), confirmed against the real Swift
// compiler while designing this plan).
func (r *renderer) renderObjectDecl(name string, o *ir.ObjectNode, cyclic bool) (string, []extraDecl, error) {
	fields, extra := r.resolveFields(name, o.Fields)

	var sb strings.Builder
	kind := "struct"
	if cyclic {
		kind = "class"
	}
	fmt.Fprintf(&sb, "public %s %s: Codable {\n", kind, name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "    public var %s: %s\n", f.propertyName, f.typeExpr)
	}
	sb.WriteString("\n    enum CodingKeys: String, CodingKey {\n")
	for _, f := range fields {
		fmt.Fprintf(&sb, "        case %s = %s\n", f.propertyName, strconv.Quote(f.wireName))
	}
	sb.WriteString("    }\n")

	if cyclic {
		sb.WriteString("\n    public init(")
		for i, f := range fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s: %s", f.propertyName, f.typeExpr)
		}
		sb.WriteString(") {\n")
		for _, f := range fields {
			fmt.Fprintf(&sb, "        self.%s = %s\n", f.propertyName, f.propertyName)
		}
		sb.WriteString("    }\n")

		sb.WriteString("\n    public required init(from decoder: Decoder) throws {\n")
		sb.WriteString("        let container = try decoder.container(keyedBy: CodingKeys.self)\n")
		for _, f := range fields {
			base := strings.TrimSuffix(f.typeExpr, "?")
			if f.optional {
				fmt.Fprintf(&sb, "        self.%s = try container.decodeIfPresent(%s.self, forKey: .%s)\n", f.propertyName, base, f.propertyName)
			} else {
				fmt.Fprintf(&sb, "        self.%s = try container.decode(%s.self, forKey: .%s)\n", f.propertyName, base, f.propertyName)
			}
		}
		sb.WriteString("    }\n")
	}

	sb.WriteString("}\n")
	return sb.String(), extra, nil
}
```

- [ ] **Step 3: Write `internal/gen/swift/generate_test.go`**

```go
package swift_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/swift"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

func buildDoc(t *testing.T, raw *spec.RawDocument) *ir.Document {
	t.Helper()
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}
	return doc
}

func TestGenerate_SimpleObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name":       {Type: "string"},
					"photo_urls": {Type: "array", Items: &spec.RawSchema{Type: "string"}},
				},
				Required: []string{"name", "photo_urls"},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	decl := models[0].Declaration
	for _, want := range []string{
		"public struct Pet: Codable {",
		"public var name: String",
		"public var photoUrls: [String]",
		`case photoUrls = "photo_urls"`,
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("declaration missing %q, got:\n%s", want, decl)
		}
	}
}

func TestGenerate_OptionalAndNullableBothBecomeOptional(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"nickname": {Type: "string", Nullable: true},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(models[0].Declaration, "public var nickname: String?") {
		t.Errorf("expected nickname: String?, got:\n%s", models[0].Declaration)
	}
}

func TestGenerate_CyclicModelIsClass(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"TreeNode": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"value":    {Type: "string"},
					"children": {Type: "array", Items: &spec.RawSchema{Ref: "#/components/schemas/TreeNode"}},
				},
				Required: []string{"value"},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "public class TreeNode: Codable {") {
		t.Errorf("expected a class for the cyclic model, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public required init(from decoder: Decoder) throws {") {
		t.Errorf("expected a hand-written init(from:) for the cyclic class, got:\n%s", decl)
	}
}

func TestGenerate_NonCyclicModelIsStruct(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(models[0].Declaration, "public struct Pet: Codable {") {
		t.Errorf("expected a struct for the non-cyclic model, got:\n%s", models[0].Declaration)
	}
}

func TestGenerate_InlineEnumSynthesizesNamedType(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"status": {Type: "string", Enum: []interface{}{"available", "pending", "sold"}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 declarations (Pet + synthesized PetStatus), got %d", len(models))
	}
	var petDecl, statusDecl string
	for _, m := range models {
		switch m.Name {
		case "Pet":
			petDecl = m.Declaration
		case "PetStatus":
			statusDecl = m.Declaration
		}
	}
	if !strings.Contains(petDecl, "public var status: PetStatus?") {
		t.Errorf("expected Pet.status to reference the synthesized PetStatus type, got:\n%s", petDecl)
	}
	if !strings.Contains(statusDecl, "public enum PetStatus: String, Codable {") {
		t.Fatalf("expected a synthesized PetStatus enum, got declarations: %+v", models)
	}
	for _, want := range []string{"case available", "case pending", "case sold"} {
		if !strings.Contains(statusDecl, want) {
			t.Errorf("PetStatus missing %q, got:\n%s", want, statusDecl)
		}
	}
}

func TestGenerate_UnionFieldIsSkippedWithModelStillGenerated(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name": {Type: "string"},
					"weird": {
						OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}},
					},
				},
				Required: []string{"name"},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "weird") {
		t.Errorf("expected the union-typed field to be omitted entirely, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public var name: String") {
		t.Errorf("expected the model's other field to still be generated, got:\n%s", decl)
	}
}

func TestGenerate_UnionModelIsSkippedEntirely(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Pet":            {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 || models[0].Name != "Pet" {
		t.Fatalf("expected only Pet to be generated (StringOrNumber skipped), got: %+v", models)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/gen/swift/... -v`
Expected: the package builds and every test PASSES.

- [ ] **Step 5: Wire `GenerateSwift` into `pkg/engine`**

Open `pkg/engine/output.go`. Add `"github.com/tarikomercehajic/openapi2code/internal/gen/swift"` to the import block, then add after `GenerateZod`'s closing brace (after the existing `modularZodOutput` function, at the end of the file):

```go
// SwiftOptions controls GenerateSwift's output layout.
type SwiftOptions struct {
	Modular bool
}

// GenerateSwift renders doc as Swift Codable structs/classes, laid out
// per opts.
//
//   - Monolithic (opts.Modular false): exactly one file, keyed
//     "Generated.swift", holding every declaration in the order
//     swift.Generate produced them. Declaration order never matters in
//     Swift — types may reference each other regardless of which comes
//     first in the file — so no topological sort is needed here, unlike
//     GenerateZod's monolithic output.
//   - Modular (opts.Modular true): one file per declaration, keyed
//     "<Name>.swift". Unlike GenerateTS/GenerateZod, there is no barrel
//     file — Swift files in one compilation target see every other
//     type automatically, so a re-export file would serve no purpose.
func GenerateSwift(doc *ir.Document, opts SwiftOptions) (Output, error) {
	models, err := swift.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularSwiftOutput(models), nil
	}
	return monolithicSwiftOutput(models), nil
}

func monolithicSwiftOutput(models []swift.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"Generated.swift": sb.String()}}
}

func modularSwiftOutput(models []swift.ModelOutput) Output {
	files := make(map[string]string, len(models))
	for _, m := range models {
		files[m.Name+".swift"] = m.Declaration
	}
	return Output{Files: files}
}
```

- [ ] **Step 6: Add Swift cases to the golden-fixture test and generate the goldens**

Open `pkg/engine/golden_test.go`. Find the table-driven test's case list (it iterates fixtures under `testdata/*.yaml` and compares against `testdata/golden/<fixture>.<target>.txt`-style files for `ts` and `zod` — read the existing test body first to see its exact loop/table shape, since you must add Swift entries in the same shape, not a parallel implementation). Add Swift monolithic-output cases for these four existing fixtures: `petstore.yaml`, `circular.yaml`, `composition.yaml`, `nullable_enum.yaml`, following the exact pattern the existing `ts`/`zod` cases already use for those same fixture files, calling `engine.GenerateSwift(doc, engine.SwiftOptions{Modular: false})` and comparing `out.Files["Generated.swift"]` against a golden file at `testdata/golden/<fixture-basename>.swift.txt`.

Run: `go test ./pkg/engine/... -run TestGoldenFixtures -update`
Expected: the four new `.swift.txt` golden files are created under `pkg/engine/testdata/golden/`.

**Read every generated golden file** before proceeding. Confirm by eye: `composition.yaml`'s `Dog` (allOf `Animal` + own `breed`) has both `name` and `breed` as flattened `public var` properties with no inheritance syntax; `composition.yaml`'s `StringOrNumber` (a top-level oneOf) does not appear in the golden at all (skipped per the design); `composition.yaml`'s `Pet` (a top-level anyOf of `Dog`/`Animal`) is also absent; `circular.yaml`'s `TreeNode` is a `class` with a hand-written `init(from:)`, and its `A`/`B` mutual-cycle pair are also both classes; `petstore.yaml`'s `Pet.status` references a synthesized `PetStatus` enum declaration also present in the same file. If anything looks wrong, fix `internal/gen/swift/generate.go` and regenerate — do not hand-edit a golden file to paper over a generator bug.

The golden cases above only exercise monolithic output (`Modular: false`); `modularSwiftOutput` (Step 5) has no test coverage yet. Close that gap now: open `pkg/engine/engine_test.go` (read its existing `TestGenerateTS_Modular_*` tests first for the file conventions it already uses, e.g. `fileNames(out.Files)` for failure messages) and add:

```go
func TestGenerateSwift_Modular_OneFilePerModelNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(petstoreSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateSwift(doc, engine.SwiftOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateSwift: %v", err)
	}
	if _, ok := out.Files["index.swift"]; ok {
		t.Error("expected no barrel file for modular Swift output")
	}
	petFile, ok := out.Files["Pet.swift"]
	if !ok {
		t.Fatalf("expected Pet.swift, got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "public struct Pet: Codable {") {
		t.Errorf("expected Pet.swift to contain the Pet struct, got:\n%s", petFile)
	}
	if strings.Contains(petFile, "import") {
		t.Errorf("expected no import statements in modular Swift output (same-target visibility needs none), got:\n%s", petFile)
	}
}
```

If this test file has no existing `petstoreSpec`-style constant already in scope from the TS/Zod tests above it, reuse whichever existing spec constant those tests already call `engine.Parse` on for a "Pet" model (check `TestGenerateTS_Modular_CollidingNamesKeepBothModels` and its neighbors for the exact constant name in scope) rather than defining a new one — the test only needs a spec with a single simple `Pet` object model.

- [ ] **Step 7: Run the full test suite**

Run: `go test ./pkg/engine/... -v`
Expected: all tests, including the new Swift golden cases, PASS.

- [ ] **Step 8: Wire `swift` into the CLI**

Open `internal/cli/parse.go`. Change line 36's `usageLine` from:
```go
const usageLine = "usage: openapi2code pull <source> [--target <ts|zod>] (--output <dir> | --out-file <path>)"
```
to:
```go
const usageLine = "usage: openapi2code pull <source> [--target <ts|zod|swift>] (--output <dir> | --out-file <path>)"
```

Change line 56's flag description from:
```go
	target := fs.String("target", "ts", `generation target: "ts" or "zod"`)
```
to:
```go
	target := fs.String("target", "ts", `generation target: "ts", "zod", or "swift"`)
```

Change lines 81-83 from:
```go
	if *target != "ts" && *target != "zod" {
		return Options{}, fmt.Errorf("target %q not yet implemented (only \"ts\" and \"zod\" are currently supported)", *target)
	}
```
to:
```go
	if *target != "ts" && *target != "zod" && *target != "swift" {
		return Options{}, fmt.Errorf("target %q not yet implemented (only \"ts\", \"zod\", and \"swift\" are currently supported)", *target)
	}
```

Open `internal/cli/run.go`. Change lines 38-43 from:
```go
	var out engine.Output
	if opts.Target == "zod" {
		out, err = engine.GenerateZod(doc, engine.ZodOptions{Modular: opts.OutDir != ""})
	} else {
		out, err = engine.GenerateTS(doc, engine.TSOptions{Modular: opts.OutDir != ""})
	}
```
to:
```go
	var out engine.Output
	switch opts.Target {
	case "zod":
		out, err = engine.GenerateZod(doc, engine.ZodOptions{Modular: opts.OutDir != ""})
	case "swift":
		out, err = engine.GenerateSwift(doc, engine.SwiftOptions{Modular: opts.OutDir != ""})
	default:
		out, err = engine.GenerateTS(doc, engine.TSOptions{Modular: opts.OutDir != ""})
	}
```

- [ ] **Step 9: Update the existing CLI tests and run the full suite**

Search `internal/cli/*_test.go` for any test asserting on the literal `usageLine` string or the old "only \"ts\" and \"zod\"" error message text (both changed in Step 8) and update those assertions to match the new text exactly. Add one new end-to-end test to `internal/cli/run_test.go` (read its existing tests first for the exact helper functions/table shape it already uses, e.g. for the existing `--target zod` end-to-end test) covering `--target swift` producing a `.swift` file with the expected content for a simple fixture.

Run: `go build ./... && go vet ./... && go test ./...`
Expected: every package passes.

- [ ] **Step 10: Commit**

```bash
git add internal/gen/swift/ pkg/engine/output.go pkg/engine/golden_test.go pkg/engine/testdata/golden/*.swift.txt internal/cli/parse.go internal/cli/run.go internal/cli/*_test.go
git commit -m "$(cat <<'EOF'
Add the Swift generator, wired into pkg/engine and the CLI

internal/gen/swift renders Codable structs (classes only for cyclic
models), synthesizing a named top-level type for any inline enum or
object a field resolves to, and skipping oneOf/anyOf/allOf-fallback
shapes cleanly. --target swift is now accepted end to end.

EOF
)"
```

---

### Task 3: Swift opt-in real-compilation verification test

**Files:**
- Create: `pkg/engine/golden_swift_typecheck_test.go`

**Interfaces:**
- Consumes: the `*.swift.txt` golden files Task 2 created under `pkg/engine/testdata/golden/`.
- Produces: nothing further downstream — this is a standalone verification test.

- [ ] **Step 1: Write the test**

Create `pkg/engine/golden_swift_typecheck_test.go`:

```go
package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoldenSwiftOutput_Compiles hands every committed *.swift.txt golden
// to the real Swift compiler. Every other test in this package compares
// generated Swift as Go strings, which cannot tell whether the output is
// valid Swift at all — this is the Swift equivalent of
// golden_zod_typecheck_test.go's tsc check.
//
// It skips (never fails) when swiftc is missing or -short is set — those
// are environment problems, not code defects.
func TestGoldenSwiftOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping swiftc compile check of Swift goldens in -short mode")
	}
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("swiftc not found on PATH; skipping compile check of Swift goldens (install Swift to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.swift.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.swift.txt golden files found to compile")
	}

	dir := t.TempDir()
	var swiftFiles []string
	for _, golden := range goldens {
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		base := strings.TrimSuffix(filepath.Base(golden), ".txt")
		dest := filepath.Join(dir, base)
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
		swiftFiles = append(swiftFiles, dest)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	args := append([]string{"-typecheck"}, swiftFiles...)
	cmd := exec.CommandContext(ctx, swiftc, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("generated Swift output does not compile (swiftc -typecheck over %d golden files):\n%s",
			len(goldens), strings.TrimSpace(string(out)))
	}
}
```

Each golden file was generated as a standalone monolithic file (Task 2's `monolithicSwiftOutput`), so every `*.swift.txt` is a self-contained, independently-compilable Swift source file — passing all of them to one `swiftc -typecheck` invocation compiles each as its own top-level module-less file, matching how a real consumer would use one such file.

- [ ] **Step 2: Run the test**

Run: `go test ./pkg/engine/... -run TestGoldenSwiftOutput_Compiles -v`
Expected: PASS (swiftc is installed in this environment, so this actually runs rather than skipping). If it fails, the failure output names the exact compiler error and file — fix `internal/gen/swift/generate.go` (Task 2) and regenerate the affected golden(s) with `go test ./pkg/engine/... -run TestGoldenFixtures -update`, then re-run this test.

- [ ] **Step 3: Run the full suite once more**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green, including the new Swift compile check.

- [ ] **Step 4: Commit**

```bash
git add pkg/engine/golden_swift_typecheck_test.go
git commit -m "$(cat <<'EOF'
Add an opt-in swiftc compile check for the Swift generator's goldens

Mirrors golden_zod_typecheck_test.go's tsc check: every committed
*.swift.txt golden is handed to the real Swift compiler. Skips
cleanly without swiftc; runs for real in this environment, where it
is installed.

EOF
)"
```

---

### Task 4: Kotlin generator, engine wiring, CLI wiring, and golden tests

**Files:**
- Create: `internal/gen/kotlin/identifier.go`
- Create: `internal/gen/kotlin/generate.go`
- Create: `internal/gen/kotlin/generate_test.go`
- Modify: `pkg/engine/output.go` (add `GenerateKotlin`/`KotlinOptions`)
- Modify: `internal/cli/parse.go` (accept `kotlin` as a `--target` value)
- Modify: `internal/cli/run.go` (dispatch `kotlin` to `engine.GenerateKotlin`)
- Create: `pkg/engine/testdata/golden/petstore.kt.txt`, `pkg/engine/testdata/golden/circular.kt.txt`, `pkg/engine/testdata/golden/composition.kt.txt`, `pkg/engine/testdata/golden/nullable_enum.kt.txt`
- Modify: `pkg/engine/golden_test.go` (add Kotlin cases)

**Interfaces:**
- Consumes: `ir.Document`/`ir.Model`/`ir.Node`; `mobile.ToCamelCase`/`ToPascalCase`/`ToScreamingSnakeCase` (Task 1). Does not depend on Task 2/3's Swift package — same architectural pattern, independent implementation.
- Produces: `kotlin.ModelOutput{Name string, Declaration string}`, `kotlin.Generate(doc *ir.Document) ([]kotlin.ModelOutput, error)`; `engine.GenerateKotlin(doc *ir.Document, opts engine.KotlinOptions) (engine.Output, error)`, `engine.KotlinOptions{Modular bool}`.

Kotlin never needs cyclic-only class handling (see Global Constraints) — every model is always a `data class`. This removes an entire dimension of complexity Task 2 had to handle; the main new complexity here is generating `toJson()`/`fromJson(...)` by hand, since Kotlin has no built-in reflection-based JSON.

- [ ] **Step 1: Write `internal/gen/kotlin/identifier.go`**

```go
// Package kotlin generates Kotlin data classes from the IR.
package kotlin

import (
	"regexp"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Kotlin hard keywords that cannot be used as a bare
// identifier.
var reservedWords = map[string]bool{
	"as": true, "break": true, "class": true, "continue": true,
	"do": true, "else": true, "false": true, "for": true, "fun": true,
	"if": true, "in": true, "interface": true, "is": true, "null": true,
	"object": true, "package": true, "return": true, "super": true,
	"this": true, "throw": true, "true": true, "try": true, "typealias": true,
	"typeof": true, "val": true, "var": true, "when": true, "while": true,
}

func sanitize(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	if reservedWords[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// SanitizeTypeIdentifier converts a model or synthesized type name into a
// valid Kotlin type identifier. Casing is left as-is.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Kotlin property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// SanitizeEnumConstantIdentifier converts a JSON enum value into a valid,
// idiomatic Kotlin enum constant identifier (SCREAMING_SNAKE_CASE by
// community convention).
func SanitizeEnumConstantIdentifier(value string) string {
	return sanitize(mobile.ToScreamingSnakeCase(value))
}
```

- [ ] **Step 2: Write `internal/gen/kotlin/generate.go`**

```go
package kotlin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Kotlin declaration: a data
// class, an enum class, or a typealias.
type ModelOutput struct {
	Name        string
	Declaration string
}

type extraDecl struct {
	name string
	node *ir.Node
}

// modelKind classifies a $ref target so fromJson/toJson know which
// expression shape to generate for it (a nested ref could point at
// either an object or an enum, which need different serialization code).
type modelKind int

const (
	modelKindObject modelKind = iota
	modelKindEnum
	modelKindOther
)

// Generate renders every model in doc as a Kotlin declaration, plus any
// declarations synthesized for inline enums/objects, mirroring
// internal/gen/swift's Generate exactly in structure (see that package's
// comments for the full rationale — this is an independent
// implementation of the same design, not a shared one, since Kotlin's
// syntax and serialization approach differ enough that sharing code would
// cost more in indirection than it saves).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	names := assignNames(doc.Models)
	used := make(map[string]bool, len(doc.Models))
	for _, n := range names {
		used[n] = true
	}
	kinds := make(map[string]modelKind, len(doc.Models))
	for _, m := range doc.Models {
		kinds[names[m.Name]] = kindOf(m.Type)
	}
	modelsByName := make(map[string]*ir.Model, len(doc.Models))
	for _, m := range doc.Models {
		modelsByName[m.Name] = m
	}
	r := &renderer{names: names, kinds: kinds, modelsByName: modelsByName}

	type queueItem struct {
		name string
		node *ir.Node
	}
	queue := make([]queueItem, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, queueItem{name: names[m.Name], node: m.Type})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, extra, err := r.renderDeclaration(item.name, item.node)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.name, err)
		}
		if decl == "" {
			continue
		}
		outputs = append(outputs, ModelOutput{Name: item.name, Declaration: decl})
		for _, e := range extra {
			name := uniquify(SanitizeTypeIdentifier(e.name), used)
			used[name] = true
			r.kinds[name] = kindOf(e.node)
			queue = append(queue, queueItem{name: name, node: e.node})
		}
	}
	return outputs, nil
}

func kindOf(node *ir.Node) modelKind {
	switch node.Kind {
	case ir.KindObject:
		return modelKindObject
	case ir.KindEnum:
		return modelKindEnum
	default:
		return modelKindOther
	}
}

func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{}
	names := make(map[string]string, len(models))
	for _, m := range models {
		name := uniquify(SanitizeTypeIdentifier(m.Name), used)
		used[name] = true
		names[m.Name] = name
	}
	return names
}

func uniquify(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
}

// modelsByName looks up a document's top-level models by their IR name
// (not their resolved Kotlin identifier) — so flattenFields can resolve
// an allOf-derived Extends chain into the actual base model's Fields.
type renderer struct {
	names        map[string]string
	kinds        map[string]modelKind
	modelsByName map[string]*ir.Model
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the design's "allOf always flattens" rule — Kotlin
// data classes model no inheritance here (data classes cannot extend
// each other), so a base's fields must be copied in directly. o.Fields
// alone is NOT already flattened: internal/ir's buildAllOfNode records a
// $ref allOf member only in Extends, never in Fields — only inline
// object members land in Fields. Extends targets are walked first, in
// order, then o's own fields, with first-occurrence-wins de-duplication
// by field name (matching ir.buildAllOfNode's own addFields precedence).
func (r *renderer) flattenFields(o *ir.ObjectNode) []*ir.Field {
	seen := make(map[string]bool)
	var fields []*ir.Field
	add := func(fs []*ir.Field) {
		for _, f := range fs {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}
	visited := make(map[string]bool, len(o.Extends))
	for _, e := range o.Extends {
		add(r.flattenExtendsTarget(e, visited))
	}
	add(o.Fields)
	return fields
}

// flattenExtendsTarget returns modelName's full field list (recursively
// flattening its own Extends, if any). visited guards against a
// malformed/cyclic Extends graph turning into infinite recursion.
func (r *renderer) flattenExtendsTarget(modelName string, visited map[string]bool) []*ir.Field {
	if visited[modelName] {
		return nil
	}
	visited[modelName] = true
	m, ok := r.modelsByName[modelName]
	if !ok || m.Type == nil || m.Type.Kind != ir.KindObject {
		return nil
	}
	var fields []*ir.Field
	for _, e := range m.Type.Object.Extends {
		fields = append(fields, r.flattenExtendsTarget(e, visited)...)
	}
	return append(fields, m.Type.Object.Fields...)
}

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

func (r *renderer) renderDeclaration(name string, node *ir.Node) (string, []extraDecl, error) {
	switch node.Kind {
	case ir.KindObject:
		return r.renderDataClass(name, node.Object)
	case ir.KindEnum:
		return r.renderEnumClass(name, node.Enum), nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return "", nil, nil
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, renderPrimitive(node.Primitive)), nil, nil
	case ir.KindArray:
		expr, skip, extra := r.resolveType(node, name)
		if skip {
			return "", nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, expr), extra, nil
	case ir.KindRef:
		return fmt.Sprintf("typealias %s = %s\n", name, r.nameFor(node.RefName)), nil, nil
	default:
		return "", nil, nil
	}
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "String"
	case ir.PrimitiveNumber:
		return "Double"
	case ir.PrimitiveInteger:
		return "Int"
	case ir.PrimitiveBoolean:
		return "Boolean"
	default:
		return "String"
	}
}

func (r *renderer) renderEnumClass(name string, e *ir.EnumNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "enum class %s(val value: String) {\n", name)
	for i, v := range e.Values {
		constName := SanitizeEnumConstantIdentifier(v)
		comma := ","
		if i == len(e.Values)-1 {
			comma = ";"
		}
		fmt.Fprintf(&sb, "    %s(%s)%s\n", constName, strconv.Quote(v), comma)
	}
	sb.WriteString("\n    companion object {\n")
	fmt.Fprintf(&sb, "        fun fromValue(value: String): %s = values().first { it.value == value }\n", name)
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String()
}

type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	node         *ir.Node // the field's own (non-optional-wrapped) resolved node, for fromJson/toJson codegen
	optional     bool
}

func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) ([]resolvedField, []extraDecl) {
	var resolved []resolvedField
	var allExtra []extraDecl
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		expr, skip, extra := r.resolveType(f.Type, suggested)
		if skip {
			continue
		}
		optional := f.Optional || f.Nullable
		typeExpr := expr
		if optional {
			typeExpr += "?"
		}
		resolved = append(resolved, resolvedField{
			propertyName: SanitizeFieldIdentifier(f.Name),
			wireName:     f.Name,
			typeExpr:     typeExpr,
			node:         effectiveNode(f.Type, suggested),
			optional:     optional,
		})
		allExtra = append(allExtra, extra...)
	}
	return resolved, allExtra
}

// effectiveNode returns the node fromJson/toJson codegen should switch
// on for this field: for an inline enum/object, that's a synthetic
// ir.KindRef pointing at the name resolveType just synthesized for it
// (renderer.kinds already has an entry for that name once resolveType's
// extraDecl has been queued), so the same ref-handling code path in
// decodeExpr/encodeExpr covers both a real $ref and a synthesized one
// uniformly.
func effectiveNode(node *ir.Node, suggestedName string) *ir.Node {
	if node.Kind == ir.KindEnum || node.Kind == ir.KindObject {
		return &ir.Node{Kind: ir.KindRef, RefName: suggestedName}
	}
	if node.Kind == ir.KindArray {
		return &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: effectiveNode(node.Array.Items, suggestedName)}}
	}
	return node
}

func (r *renderer) resolveType(node *ir.Node, suggestedName string) (expr string, skip bool, extra []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", true, nil
		}
		return renderPrimitive(node.Primitive), false, nil
	case ir.KindRef:
		return r.nameFor(node.RefName), false, nil
	case ir.KindArray:
		itemExpr, itemSkip, itemExtra := r.resolveType(node.Array.Items, suggestedName)
		if itemSkip {
			return "", true, nil
		}
		return "List<" + itemExpr + ">", false, itemExtra
	case ir.KindEnum, ir.KindObject:
		return suggestedName, false, []extraDecl{{name: suggestedName, node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return "", true, nil
	default:
		return "", true, nil
	}
}

// renderDataClass renders o as a data class. o.Fields alone is not yet
// flattened for an allOf whose members include a $ref (see
// flattenFields), so this resolves the full field list through
// flattenFields rather than reading o.Fields directly.
func (r *renderer) renderDataClass(name string, o *ir.ObjectNode) (string, []extraDecl, error) {
	fields, extra := r.resolveFields(name, r.flattenFields(o))

	var sb strings.Builder
	fmt.Fprintf(&sb, "data class %s(\n", name)
	for i, f := range fields {
		comma := ","
		if i == len(fields)-1 {
			comma = ""
		}
		def := ""
		if f.optional {
			def = " = null"
		}
		fmt.Fprintf(&sb, "    val %s: %s%s%s\n", f.propertyName, f.typeExpr, def, comma)
	}
	sb.WriteString(") {\n")

	sb.WriteString("    fun toJson(): Map<String, Any?> {\n")
	sb.WriteString("        val map = mutableMapOf<String, Any?>()\n")
	for _, f := range fields {
		fmt.Fprintf(&sb, "        map[%s] = %s\n", strconv.Quote(f.wireName), r.toJsonExpr(f))
	}
	sb.WriteString("        return map\n")
	sb.WriteString("    }\n")

	sb.WriteString("\n    companion object {\n")
	fmt.Fprintf(&sb, "        fun fromJson(json: Map<String, Any?>): %s {\n", name)
	fmt.Fprintf(&sb, "            return %s(\n", name)
	for i, f := range fields {
		comma := ","
		if i == len(fields)-1 {
			comma = ""
		}
		fmt.Fprintf(&sb, "                %s = %s%s\n", f.propertyName, r.fromJsonExpr(f), comma)
	}
	sb.WriteString("            )\n")
	sb.WriteString("        }\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String(), extra, nil
}

// toJsonExpr renders the RHS of `map["<wire>"] = <expr>` for one field.
// Primitives serialize as themselves; a nested object serializes via its
// own toJson(); an enum serializes via its .value property (consulting
// r.kinds, populated in Generate, to tell an object-shaped $ref from an
// enum-shaped one); a List maps element-wise; every case has an optional
// variant using ?. instead of a direct call.
func (r *renderer) toJsonExpr(f resolvedField) string {
	access := f.propertyName
	if f.optional {
		access += "?"
	}
	switch f.node.Kind {
	case ir.KindRef:
		if r.kinds[f.node.RefName] == modelKindEnum {
			return access + ".value"
		}
		return access + ".toJson()"
	case ir.KindArray:
		return fmt.Sprintf("%s%s { %s }", access, mapOp(f.optional), r.itemToJsonExpr(f.node.Array.Items))
	default:
		return f.propertyName
	}
}

func mapOp(optional bool) string {
	if optional {
		return "?.map"
	}
	return ".map"
}

// itemToJsonExpr renders the body of the `.map { ... }` lambda used to
// serialize one array element.
func (r *renderer) itemToJsonExpr(item *ir.Node) string {
	if item.Kind == ir.KindRef {
		if r.kinds[item.RefName] == modelKindEnum {
			return "it.value"
		}
		return "it.toJson()"
	}
	return "it"
}

// fromJsonExpr renders the RHS of `<propertyName> = <expr>` inside
// fromJson's constructor call for one field. Casting a JSON number to
// the common Number supertype before converting (rather than casting
// straight to Int/Double) is deliberate: real-world JSON-to-Map parsers
// vary in whether a whole number comes through as Int, Long, or Double.
func (r *renderer) fromJsonExpr(f resolvedField) string {
	wire := strconv.Quote(f.wireName)
	switch f.node.Kind {
	case ir.KindPrimitive:
		return kotlinPrimitiveFromJsonExpr(f.node.Primitive, wire, f.optional)
	case ir.KindRef:
		target := r.nameFor(f.node.RefName)
		if r.kinds[f.node.RefName] == modelKindEnum {
			if f.optional {
				return fmt.Sprintf("(json[%s] as? String)?.let { %s.fromValue(it) }", wire, target)
			}
			return fmt.Sprintf("%s.fromValue(json[%s] as String)", target, wire)
		}
		if f.optional {
			return fmt.Sprintf("(json[%s] as? Map<String, Any?>)?.let { %s.fromJson(it) }", wire, target)
		}
		return fmt.Sprintf("%s.fromJson(json[%s] as Map<String, Any?>)", target, wire)
	case ir.KindArray:
		itemExpr := r.itemFromJsonExpr(f.node.Array.Items)
		if f.optional {
			return fmt.Sprintf("(json[%s] as? List<*>)?.map { %s }", wire, itemExpr)
		}
		return fmt.Sprintf("(json[%s] as List<*>).map { %s }", wire, itemExpr)
	default:
		return fmt.Sprintf("json[%s]", wire)
	}
}

func kotlinPrimitiveFromJsonExpr(p ir.Primitive, wire string, optional bool) string {
	switch p {
	case ir.PrimitiveBoolean:
		if optional {
			return fmt.Sprintf("json[%s] as? Boolean", wire)
		}
		return fmt.Sprintf("json[%s] as Boolean", wire)
	case ir.PrimitiveInteger:
		if optional {
			return fmt.Sprintf("(json[%s] as? Number)?.toInt()", wire)
		}
		return fmt.Sprintf("(json[%s] as Number).toInt()", wire)
	case ir.PrimitiveNumber:
		if optional {
			return fmt.Sprintf("(json[%s] as? Number)?.toDouble()", wire)
		}
		return fmt.Sprintf("(json[%s] as Number).toDouble()", wire)
	default: // ir.PrimitiveString and any unexpected value both read as a string
		if optional {
			return fmt.Sprintf("json[%s] as? String", wire)
		}
		return fmt.Sprintf("json[%s] as String", wire)
	}
}

// itemFromJsonExpr renders the body of the `.map { ... }` lambda used to
// deserialize one array element.
func (r *renderer) itemFromJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindPrimitive:
		switch item.Primitive {
		case ir.PrimitiveBoolean:
			return "it as Boolean"
		case ir.PrimitiveInteger:
			return "(it as Number).toInt()"
		case ir.PrimitiveNumber:
			return "(it as Number).toDouble()"
		default:
			return "it as String"
		}
	case ir.KindRef:
		target := r.nameFor(item.RefName)
		if r.kinds[item.RefName] == modelKindEnum {
			return fmt.Sprintf("%s.fromValue(it as String)", target)
		}
		return fmt.Sprintf("%s.fromJson(it as Map<String, Any?>)", target)
	default:
		return "it"
	}
}
```

- [ ] **Step 3: Write `internal/gen/kotlin/generate_test.go`**

```go
package kotlin_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/kotlin"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

func buildDoc(t *testing.T, raw *spec.RawDocument) *ir.Document {
	t.Helper()
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}
	return doc
}

func TestGenerate_SimpleObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name":       {Type: "string"},
					"photo_urls": {Type: "array", Items: &spec.RawSchema{Type: "string"}},
				},
				Required: []string{"name", "photo_urls"},
			},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	decl := models[0].Declaration
	for _, want := range []string{
		"data class Pet(",
		"val name: String,",
		"val photoUrls: List<String>",
		`map["photo_urls"] = photoUrls`,
		"fun fromJson(json: Map<String, Any?>): Pet {",
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("declaration missing %q, got:\n%s", want, decl)
		}
	}
}

func TestGenerate_OptionalFieldGetsNullDefault(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {Type: "object", Properties: map[string]*spec.RawSchema{"nickname": {Type: "string", Nullable: true}}},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(models[0].Declaration, "val nickname: String? = null") {
		t.Errorf("expected nickname: String? = null, got:\n%s", models[0].Declaration)
	}
}

func TestGenerate_InlineEnumSynthesizesNamedTypeWithFromValue(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"status": {Type: "string", Enum: []interface{}{"available", "pending", "sold"}},
				},
			},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 declarations, got %d: %+v", len(models), models)
	}
	var petDecl, statusDecl string
	for _, m := range models {
		switch m.Name {
		case "Pet":
			petDecl = m.Declaration
		case "PetStatus":
			statusDecl = m.Declaration
		}
	}
	if !strings.Contains(petDecl, "val status: PetStatus? = null") {
		t.Errorf("expected Pet.status: PetStatus? = null, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `status?.value`) {
		t.Errorf("expected toJson to serialize status via .value, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "PetStatus.fromValue") {
		t.Errorf("expected fromJson to deserialize status via PetStatus.fromValue, got:\n%s", petDecl)
	}
	if !strings.Contains(statusDecl, "enum class PetStatus(val value: String) {") {
		t.Fatalf("expected a synthesized PetStatus enum class, got:\n%s", statusDecl)
	}
	for _, want := range []string{`AVAILABLE("available")`, `PENDING("pending")`, `SOLD("sold")`} {
		if !strings.Contains(statusDecl, want) {
			t.Errorf("PetStatus missing %q, got:\n%s", want, statusDecl)
		}
	}
}

func TestGenerate_RefToObjectUsesFromJsonToJson(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Category": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"category": {Ref: "#/components/schemas/Category"},
				},
				Required: []string{"category"},
			},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl string
	for _, m := range models {
		if m.Name == "Pet" {
			petDecl = m.Declaration
		}
	}
	if petDecl == "" {
		t.Fatal("Pet not found in output")
	}
	if !strings.Contains(petDecl, "category.toJson()") {
		t.Errorf("expected category.toJson() in toJson, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "Category.fromJson(json[\"category\"] as Map<String, Any?>)") {
		t.Errorf("expected Category.fromJson(...) in fromJson, got:\n%s", petDecl)
	}
}

func TestGenerate_AllOfFlattensRefBaseFields(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Animal": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Dog": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Animal"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"breed": {Type: "string"}}, Required: []string{"breed"}},
				},
			},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var dogDecl string
	for _, m := range models {
		if m.Name == "Dog" {
			dogDecl = m.Declaration
		}
	}
	if dogDecl == "" {
		t.Fatal("Dog not found in output")
	}
	if !strings.Contains(dogDecl, "val name: String,") {
		t.Errorf("expected Dog to have name flattened in from its $ref allOf member Animal, got:\n%s", dogDecl)
	}
	if !strings.Contains(dogDecl, "val breed: String") {
		t.Errorf("expected Dog to have its own breed field, got:\n%s", dogDecl)
	}
}

func TestGenerate_UnionFieldIsSkippedWithModelStillGenerated(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name":  {Type: "string"},
					"weird": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
				},
				Required: []string{"name"},
			},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(models[0].Declaration, "weird") {
		t.Errorf("expected the union-typed field to be omitted, got:\n%s", models[0].Declaration)
	}
	if !strings.Contains(models[0].Declaration, "val name: String,") && !strings.Contains(models[0].Declaration, "val name: String\n") {
		t.Errorf("expected the model's other field to still be generated, got:\n%s", models[0].Declaration)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/gen/kotlin/... -v`
Expected: PASS. `TestGenerate_RefToObjectUsesFromJsonToJson` and `TestGenerate_InlineEnumSynthesizesNamedTypeWithFromValue` are the two tests that specifically catch an object-vs-enum `$ref` dispatch mistake in `r.toJsonExpr`/`r.fromJsonExpr` — if either fails, that's where to look first.

- [ ] **Step 5: Wire `GenerateKotlin` into `pkg/engine`**

Open `pkg/engine/output.go`. Add `"github.com/tarikomercehajic/openapi2code/internal/gen/kotlin"` to the imports, then add (mirroring Task 2 Step 5's Swift wiring exactly, substituting Kotlin's package/type/extension names):

```go
// KotlinOptions controls GenerateKotlin's output layout.
type KotlinOptions struct {
	Modular bool
}

// GenerateKotlin renders doc as Kotlin data classes, laid out per opts.
// Mirrors GenerateSwift's contract: no topological sort needed for
// monolithic output (Kotlin, like Swift, resolves types regardless of
// declaration order), and no barrel file for modular output (Kotlin
// files with no package declaration share the default package and see
// each other automatically).
func GenerateKotlin(doc *ir.Document, opts KotlinOptions) (Output, error) {
	models, err := kotlin.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularKotlinOutput(models), nil
	}
	return monolithicKotlinOutput(models), nil
}

func monolithicKotlinOutput(models []kotlin.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"Generated.kt": sb.String()}}
}

func modularKotlinOutput(models []kotlin.ModelOutput) Output {
	files := make(map[string]string, len(models))
	for _, m := range models {
		files[m.Name+".kt"] = m.Declaration
	}
	return Output{Files: files}
}
```

- [ ] **Step 6: Add Kotlin cases to the golden-fixture test and generate the goldens**

Follow Task 2 Step 6's exact procedure, substituting Kotlin: add cases calling `engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: false})` for the same four fixtures, comparing `out.Files["Generated.kt"]` against `testdata/golden/<fixture-basename>.kt.txt`.

Run: `go test ./pkg/engine/... -run TestGoldenFixtures -update`

**Read every generated `.kt.txt` golden file.** Confirm: `composition.yaml`'s `Dog` has flattened `name`/`breed` properties with no inheritance; `StringOrNumber` and the top-level `Pet` (anyOf) are absent; `petstore.yaml`'s `Pet` references a synthesized `PetStatus` enum class also present in the same file, with `fromJson` calling `PetStatus.fromValue(...)`.

As in Task 2 Step 6, the golden cases above only exercise monolithic output. Add the modular-mode counterpart to `pkg/engine/engine_test.go`:

```go
func TestGenerateKotlin_Modular_OneFilePerModelNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(petstoreSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateKotlin: %v", err)
	}
	if _, ok := out.Files["index.kt"]; ok {
		t.Error("expected no barrel file for modular Kotlin output")
	}
	petFile, ok := out.Files["Pet.kt"]
	if !ok {
		t.Fatalf("expected Pet.kt, got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "data class Pet(") {
		t.Errorf("expected Pet.kt to contain the Pet data class, got:\n%s", petFile)
	}
	if strings.Contains(petFile, "import") {
		t.Errorf("expected no import statements in modular Kotlin output (default-package visibility needs none), got:\n%s", petFile)
	}
}
```

(Use the same `petstoreSpec`-equivalent constant Task 2's test reused — do not redefine it a third time.)

- [ ] **Step 7: Run the full test suite**

Run: `go test ./pkg/engine/... -v`
Expected: all PASS.

- [ ] **Step 8: Wire `kotlin` into the CLI**

Repeat Task 2 Step 8's exact edits to `internal/cli/parse.go` and `internal/cli/run.go`, this time adding `kotlin` as a third option: `usageLine` becomes `"usage: openapi2code pull <source> [--target <ts|zod|swift|kotlin>] (--output <dir> | --out-file <path>)"`, the flag description becomes `` `generation target: "ts", "zod", "swift", or "kotlin"` ``, the validation check becomes `*target != "ts" && *target != "zod" && *target != "swift" && *target != "kotlin"` with the matching error message, and `run.go`'s switch gains a `case "kotlin": out, err = engine.GenerateKotlin(doc, engine.KotlinOptions{Modular: opts.OutDir != ""})` branch.

- [ ] **Step 9: Update the CLI tests and run the full suite**

Same as Task 2 Step 9: update any test asserting the literal `usageLine`/error-message text, add one end-to-end `--target kotlin` test to `internal/cli/run_test.go`.

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 10: Commit**

```bash
git add internal/gen/kotlin/ pkg/engine/output.go pkg/engine/golden_test.go pkg/engine/testdata/golden/*.kt.txt internal/cli/parse.go internal/cli/run.go internal/cli/*_test.go
git commit -m "$(cat <<'EOF'
Add the Kotlin generator, wired into pkg/engine and the CLI

internal/gen/kotlin renders plain data classes with hand-written
toJson()/fromJson() (no reflection-based JSON exists in Kotlin's
standard library, and this avoids requiring the consumer to add
kotlinx-serialization's dependency and compiler plugin). Same
inline-enum/object synthesis and unsupported-shape skipping as the
Swift generator; no cyclic special-casing needed (Kotlin objects are
always references). --target kotlin is now accepted end to end.

EOF
)"
```

---

### Task 5: Kotlin opt-in real-compilation verification test

**Files:**
- Create: `pkg/engine/golden_kotlin_typecheck_test.go`

**Interfaces:**
- Consumes: the `*.kt.txt` golden files Task 4 created.
- Produces: nothing further downstream.

- [ ] **Step 1: Write the test**

Create `pkg/engine/golden_kotlin_typecheck_test.go`:

```go
package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoldenKotlinOutput_Compiles hands every committed *.kt.txt golden
// to the real Kotlin compiler, mirroring golden_swift_typecheck_test.go.
// Skips (never fails) when kotlinc is missing or -short is set — this
// environment does not have kotlinc installed, so this test skips here
// today but runs for real wherever it is.
func TestGoldenKotlinOutput_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping kotlinc compile check of Kotlin goldens in -short mode")
	}
	kotlinc, err := exec.LookPath("kotlinc")
	if err != nil {
		t.Skip("kotlinc not found on PATH; skipping compile check of Kotlin goldens (install Kotlin to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.kt.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.kt.txt golden files found to compile")
	}

	dir := t.TempDir()
	var ktFiles []string
	for _, golden := range goldens {
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		base := strings.TrimSuffix(filepath.Base(golden), ".txt")
		dest := filepath.Join(dir, base)
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
		ktFiles = append(ktFiles, dest)
	}

	// Each golden is compiled to its own output jar in a subdirectory
	// (named after the golden's base name) so that duplicate top-level
	// declarations across different goldens — every fixture has its own
	// Pet, Category, etc. — never collide in one shared classpath.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, ktFile := range ktFiles {
		outJar := ktFile + ".jar"
		cmd := exec.CommandContext(ctx, kotlinc, ktFile, "-d", outJar)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("generated Kotlin output does not compile (%s):\n%s", filepath.Base(ktFile), strings.TrimSpace(string(out)))
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./pkg/engine/... -run TestGoldenKotlinOutput_Compiles -v`
Expected: SKIP in this environment (`kotlinc` is not installed here — this was confirmed while designing this plan). The skip message must clearly say why, exactly like the code above.

- [ ] **Step 3: Run the full suite once more**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green (the new test contributes a skip, not a failure).

- [ ] **Step 4: Commit**

```bash
git add pkg/engine/golden_kotlin_typecheck_test.go
git commit -m "$(cat <<'EOF'
Add an opt-in kotlinc compile check for the Kotlin generator's goldens

Mirrors the Swift/Zod typecheck tests. Skips cleanly without kotlinc
(not installed in this environment); runs for real wherever it is.

EOF
)"
```

---

### Task 6: Dart generator, engine wiring, CLI wiring, and golden tests

**Files:**
- Create: `internal/gen/dart/identifier.go`
- Create: `internal/gen/dart/generate.go`
- Create: `internal/gen/dart/generate_test.go`
- Modify: `pkg/engine/output.go` (add `GenerateDart`/`DartOptions`)
- Modify: `internal/cli/parse.go` (accept `dart` as a `--target` value)
- Modify: `internal/cli/run.go` (dispatch `dart` to `engine.GenerateDart`)
- Create: `pkg/engine/testdata/golden/petstore.dart.txt`, `pkg/engine/testdata/golden/circular.dart.txt`, `pkg/engine/testdata/golden/composition.dart.txt`, `pkg/engine/testdata/golden/nullable_enum.dart.txt`
- Modify: `pkg/engine/golden_test.go` (add Dart cases)

**Interfaces:**
- Consumes: `ir.Document`/`ir.Model`/`ir.Node`; `mobile.ToCamelCase`/`ToPascalCase` (Task 1).
- Produces: `dart.ModelOutput{Name string, Declaration string, Dependencies []string}` — Dart is the one language of the three that needs `Dependencies`, since (unlike Swift/Kotlin) Dart requires an explicit `import` between files even in the same package; `dart.Generate(doc *ir.Document) ([]dart.ModelOutput, error)`; `engine.GenerateDart(doc *ir.Document, opts engine.DartOptions) (engine.Output, error)`, `engine.DartOptions{Modular bool}`.

Dart, like Kotlin, needs no cyclic special-casing (Dart objects are always references) — every model is a plain `class`. Dart's own wrinkle: conventional Dart **file** names are `snake_case` (e.g. `tree_node.dart`) even though the **class** name stays `TreeNode` — this only matters for modular output's file naming and import statements, not for any generated Dart source syntax itself.

- [ ] **Step 1: Write `internal/gen/dart/identifier.go`**

```go
// Package dart generates Dart classes from the IR.
package dart

import (
	"regexp"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Dart reserved words that cannot be used as a bare
// identifier.
var reservedWords = map[string]bool{
	"assert": true, "break": true, "case": true, "catch": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "else": true, "enum": true, "extends": true, "false": true,
	"final": true, "finally": true, "for": true, "if": true, "in": true,
	"is": true, "new": true, "null": true, "rethrow": true, "return": true,
	"super": true, "switch": true, "this": true, "throw": true,
	"true": true, "try": true, "var": true, "void": true, "while": true,
	"with": true,
}

func sanitize(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	if reservedWords[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// SanitizeTypeIdentifier converts a model or synthesized type name into a
// valid Dart class identifier. Casing is left as-is.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Dart property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// SanitizeEnumValueIdentifier converts a JSON enum value into a valid,
// idiomatic Dart enum value identifier (lowerCamelCase — Dart's official
// style guide uses lowerCamelCase for enum values, unlike Kotlin/Java's
// SCREAMING_SNAKE_CASE convention).
func SanitizeEnumValueIdentifier(value string) string {
	return sanitize(mobile.ToCamelCase(value))
}

// FileName converts a Dart class name into its conventional snake_case
// file name (TreeNode -> tree_node.dart), independent of the class name's
// own PascalCase spelling.
func FileName(typeName string) string {
	var sb strings.Builder
	runes := []rune(typeName)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteRune('_')
			}
			sb.WriteRune(r - 'A' + 'a')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String() + ".dart"
}
```

- [ ] **Step 2: Write `internal/gen/dart/generate.go`**

```go
package dart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Dart declaration: a class, an
// enhanced enum, or a typedef. Dependencies lists the other declarations
// (by their Dart type name) this one's declaration text references —
// needed because, unlike Swift/Kotlin, Dart requires an explicit import
// between files even within the same package.
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
}

type extraDecl struct {
	name string
	node *ir.Node
}

type modelKind int

const (
	modelKindObject modelKind = iota
	modelKindEnum
	modelKindOther
)

// Generate renders every model in doc as a Dart declaration, plus any
// synthesized for inline enums/objects — the same structure as
// internal/gen/swift and internal/gen/kotlin's Generate, independently
// implemented for Dart's syntax and serialization approach.
// Generate mints every synthesized declaration's name and classifies its
// modelKind at the exact moment resolveType creates it (see resolveType's
// doc comment) — not later, in this function's own extra-processing loop.
// An earlier version of this plan did exactly that later re-derivation,
// which is a real, reproduced bug (caught during Task 4's Kotlin
// implementation and fixed there before Dart's brief was written): a
// declaration's own toJsonExpr/fromJsonExpr runs synchronously, before
// this loop ever reaches the matching extra entry, and already needs
// r.kinds populated for any inline enum/object that declaration just
// synthesized.
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	names := assignNames(doc.Models)
	used := make(map[string]bool, len(doc.Models))
	for _, n := range names {
		used[n] = true
	}
	kinds := make(map[string]modelKind, len(doc.Models))
	for _, m := range doc.Models {
		kinds[names[m.Name]] = kindOf(m.Type)
	}
	modelsByName := make(map[string]*ir.Model, len(doc.Models))
	for _, m := range doc.Models {
		modelsByName[m.Name] = m
	}
	r := &renderer{names: names, kinds: kinds, modelsByName: modelsByName, used: used}

	type queueItem struct {
		name string
		node *ir.Node
	}
	queue := make([]queueItem, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, queueItem{name: names[m.Name], node: m.Type})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, deps, extra, err := r.renderDeclaration(item.name, item.node)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.name, err)
		}
		if decl == "" {
			continue
		}
		outputs = append(outputs, ModelOutput{Name: item.name, Declaration: decl, Dependencies: dedupeSorted(removeName(deps, item.name))})
		for _, e := range extra {
			// e.name is already final and globally unique, and r.kinds[e.name]
			// is already populated — resolveType did both at the moment it
			// synthesized this extraDecl. Do not re-derive either here.
			queue = append(queue, queueItem{name: e.name, node: e.node})
		}
	}
	return outputs, nil
}

func kindOf(node *ir.Node) modelKind {
	switch node.Kind {
	case ir.KindObject:
		return modelKindObject
	case ir.KindEnum:
		return modelKindEnum
	default:
		return modelKindOther
	}
}

func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{}
	names := make(map[string]string, len(models))
	for _, m := range models {
		name := uniquify(SanitizeTypeIdentifier(m.Name), used)
		used[name] = true
		names[m.Name] = name
	}
	return names
}

func uniquify(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
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
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
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

// modelsByName looks up a document's top-level models by their IR name —
// so flattenFields can resolve an allOf-derived Extends chain into the
// actual base model's Fields.
type renderer struct {
	names        map[string]string
	kinds        map[string]modelKind
	modelsByName map[string]*ir.Model

	// used tracks every Dart type identifier minted so far: every
	// top-level model's assigned name (seeded above before any rendering
	// starts) plus every synthesized inline enum/object name. resolveType
	// consults and updates this directly, at the exact moment it mints a
	// new synthesized name, so that name — and its r.kinds entry — are
	// decided exactly once. See resolveType's doc comment.
	used map[string]bool
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the design's "allOf always flattens" rule — Dart's
// single-inheritance `extends` can't represent a multi-member allOf
// chain anyway, so a base's fields are copied in directly instead. This
// is required, not optional: internal/ir's buildAllOfNode records a
// $ref allOf member only in Extends, never in Fields — only inline
// object members land in Fields, so o.Fields alone is NOT already
// flattened. Extends targets are walked first, in order (for a
// readable field ordering), but o's OWN fields always win a name
// collision — matching internal/gen/ts's `extends` and internal/gen/zod's
// `.extend()`, both of which let a derived schema's own field shadow an
// inherited one of the same name on the identical spec. (An earlier
// version of this function gave the base precedence instead —
// discovered and fixed during Task 2's Swift review; ownNames below is
// what fixes it.)
func (r *renderer) flattenFields(o *ir.ObjectNode) []*ir.Field {
	ownNames := make(map[string]bool, len(o.Fields))
	for _, f := range o.Fields {
		ownNames[f.Name] = true
	}

	seen := make(map[string]bool)
	var fields []*ir.Field
	addBase := func(fs []*ir.Field) {
		for _, f := range fs {
			if ownNames[f.Name] || seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}
	visited := make(map[string]bool, len(o.Extends))
	for _, e := range o.Extends {
		addBase(r.flattenExtendsTarget(e, visited))
	}
	// o.Fields is appended unconditionally, not through addBase's dedup
	// — ownNames above already guarantees nothing already in fields
	// shares a name with it.
	return append(fields, o.Fields...)
}

// flattenExtendsTarget returns modelName's full field list (recursively
// flattening its own Extends, if any). visited guards against a
// malformed/cyclic Extends graph turning into infinite recursion.
func (r *renderer) flattenExtendsTarget(modelName string, visited map[string]bool) []*ir.Field {
	if visited[modelName] {
		return nil
	}
	visited[modelName] = true
	m, ok := r.modelsByName[modelName]
	if !ok || m.Type == nil || m.Type.Kind != ir.KindObject {
		return nil
	}
	var fields []*ir.Field
	for _, e := range m.Type.Object.Extends {
		fields = append(fields, r.flattenExtendsTarget(e, visited)...)
	}
	return append(fields, m.Type.Object.Fields...)
}

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

func (r *renderer) renderDeclaration(name string, node *ir.Node) (decl string, deps []string, extra []extraDecl, err error) {
	switch node.Kind {
	case ir.KindObject:
		d, deps, extra := r.renderClass(name, node.Object)
		return d, deps, extra, nil
	case ir.KindEnum:
		return r.renderEnum(name, node.Enum), nil, nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return unsupportedShapeComment(name), nil, nil, nil
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return unsupportedShapeComment(name), nil, nil, nil
		}
		return fmt.Sprintf("typedef %s = %s;\n", name, renderPrimitive(node.Primitive)), nil, nil, nil
	case ir.KindArray:
		expr, _, skip, extra := r.resolveType(node, name)
		if skip {
			return unsupportedShapeComment(name), nil, nil, nil
		}
		return fmt.Sprintf("typedef %s = %s;\n", name, expr), collectRefDeps(node), extra, nil
	case ir.KindRef:
		target := r.nameFor(node.RefName)
		return fmt.Sprintf("typedef %s = %s;\n", name, target), []string{target}, nil, nil
	default:
		return "", nil, nil, nil
	}
}

// unsupportedShapeComment renders the comment-only declaration emitted in
// place of name's real declaration when its shape has no clean Dart
// representation (oneOf/anyOf, an allOf mixing object and non-object
// members, or an unrecognized primitive type) — per the design spec's
// "skipped ... with a comment explaining why" rule. Mirrors
// internal/gen/swift's identically-named helper.
func unsupportedShapeComment(name string) string {
	return fmt.Sprintf("// %s was not generated for the Dart target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", name)
}

// collectRefDeps walks node for every KindRef it contains (directly or
// through arrays), for the rare top-level array-of-ref typedef case.
func collectRefDeps(node *ir.Node) []string {
	switch node.Kind {
	case ir.KindRef:
		return []string{node.RefName}
	case ir.KindArray:
		return collectRefDeps(node.Array.Items)
	default:
		return nil
	}
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "String"
	case ir.PrimitiveNumber:
		return "double"
	case ir.PrimitiveInteger:
		return "int"
	case ir.PrimitiveBoolean:
		return "bool"
	default:
		return "String"
	}
}

func (r *renderer) renderEnum(name string, e *ir.EnumNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "enum %s {\n", name)
	for i, v := range e.Values {
		valueName := SanitizeEnumValueIdentifier(v)
		comma := ","
		if i == len(e.Values)-1 {
			comma = ";"
		}
		fmt.Fprintf(&sb, "    %s(%s)%s\n", valueName, strconv.Quote(v), comma)
	}
	sb.WriteString("\n    final String value;\n")
	fmt.Fprintf(&sb, "    const %s(this.value);\n\n", name)
	fmt.Fprintf(&sb, "    static %s fromValue(String value) => %s.values.firstWhere((e) => e.value == value);\n", name, name)
	sb.WriteString("}\n")
	return sb.String()
}

type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	node         *ir.Node
	optional     bool
}

// resolveFields resolves every field of an object into resolvedFields,
// synthesizing a top-level name for any inline enum/object it encounters.
// skipped carries the wire (original JSON) name of every field omitted
// because resolveType found no representable Dart type for it — per the
// design's unsupported-shape rule, the caller surfaces these as comments
// rather than dropping them without a trace (mirrors internal/gen/swift's
// resolveFields).
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) (resolved []resolvedField, deps []string, allExtra []extraDecl, skipped []string) {
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		expr, effective, skip, extra := r.resolveType(f.Type, suggested)
		if skip {
			skipped = append(skipped, f.Name)
			continue
		}
		optional := f.Optional || f.Nullable
		typeExpr := expr
		if optional {
			typeExpr += "?"
		}
		resolved = append(resolved, resolvedField{
			propertyName: SanitizeFieldIdentifier(f.Name),
			wireName:     f.Name,
			typeExpr:     typeExpr,
			node:         effective,
			optional:     optional,
		})
		deps = append(deps, refNamesIn(effective)...)
		allExtra = append(allExtra, extra...)
	}
	return resolved, deps, allExtra, skipped
}

func refNamesIn(node *ir.Node) []string {
	switch node.Kind {
	case ir.KindRef:
		return []string{node.RefName}
	case ir.KindArray:
		return refNamesIn(node.Array.Items)
	default:
		return nil
	}
}

// resolveType resolves node to a non-optional Dart type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object. skip is true when node has no
// representable Dart type (a union, an allOf-with-non-object-member
// intersection, or an unrecognized primitive) — the caller omits
// whatever position this type would have occupied.
//
// effective is the node fromJson/toJson codegen (toJsonExpr/fromJsonExpr)
// should switch on for this field: for a real $ref it's node itself; for
// an inline enum/object it's a synthetic ir.KindRef pointing at the exact
// name minted just below, so the same ref-handling code path covers both
// a real $ref and a synthesized one uniformly; for an array it recurses
// per element.
//
// For the ir.KindEnum/ir.KindObject case, the name a synthesized
// declaration is finally enqueued under (in Generate's queue) — and the
// modelKind recorded for it in r.kinds — must be exactly the name
// embedded in both the returned expr and the returned effective node:
// they are read by different code at different times (expr appears in
// this field's type declaration; effective is consulted later, by
// toJsonExpr/fromJsonExpr on this very same field, and again by Generate
// once this declaration's rendering has finished and it enqueues extra).
// Minting the final, unique name here — once, via r.used — and recording
// its kind in r.kinds in the same step, closes two gaps at once: a name
// collision can no longer make the type expression point at the wrong
// declaration, and toJsonExpr/fromJsonExpr can no longer observe a
// not-yet-populated r.kinds entry for a name this very call is
// synthesizing.
func (r *renderer) resolveType(node *ir.Node, suggestedName string) (expr string, effective *ir.Node, skip bool, extra []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", nil, true, nil
		}
		return renderPrimitive(node.Primitive), node, false, nil
	case ir.KindRef:
		return r.nameFor(node.RefName), r.effectiveRefNode(node), false, nil
	case ir.KindArray:
		itemExpr, itemEffective, itemSkip, itemExtra := r.resolveType(node.Array.Items, suggestedName)
		if itemSkip {
			return "", nil, true, nil
		}
		effective := &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: itemEffective}}
		return "List<" + itemExpr + ">", effective, false, itemExtra
	case ir.KindEnum, ir.KindObject:
		name := uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		r.kinds[name] = kindOf(node)
		ref := &ir.Node{Kind: ir.KindRef, RefName: name}
		return name, ref, false, []extraDecl{{name: name, node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return "", nil, true, nil
	default:
		return "", nil, true, nil
	}
}

// refKind reports the modelKind of the top-level model named refName — the
// raw IR schema name, exactly as stored on an ir.Node's RefName field.
// r.kinds itself is keyed by the resolved Dart identifier (see Generate and
// resolveType's ir.KindEnum/ir.KindObject case), so a raw name must always
// be routed through r.nameFor before it can be used to key into r.kinds.
// Looking r.kinds up directly by a raw schema name only agrees with this
// when that name happens to need no sanitization and never collided with
// anything — a schema literally named "Pet-Status", one needing a
// reserved-word suffix, or one that lost a name collision to a same-named
// sibling (picking up a "_2" suffix) would all silently mismatch and fall
// through to the wrong (object) dispatch branch. (This is the exact bug
// class found and fixed in internal/gen/kotlin's own review — applying the
// same fix here before Dart is dispatched.)
func (r *renderer) refKind(refName string) modelKind {
	return r.kinds[r.nameFor(refName)]
}

// resolveAliasTarget walks refName's own top-level model definition
// through a chain of pure $ref aliases — a schema whose entire body is
// `$ref: ...`, which internal/ir's buildNode renders as a bare
// ir.Node{Kind: KindRef} at the top level just as readily as anywhere
// else — returning the first non-$ref node it finds, together with the
// model name it was actually declared under (which may differ from
// refName when the chain is more than one link long). visited guards
// against a malformed/cyclic alias chain; ok is false when refName isn't a
// known model, has no recorded type, or the chain is cyclic.
func (r *renderer) resolveAliasTarget(refName string, visited map[string]bool) (resolved *ir.Node, resolvedName string, ok bool) {
	if visited[refName] {
		return nil, "", false
	}
	visited[refName] = true
	m, found := r.modelsByName[refName]
	if !found || m.Type == nil {
		return nil, "", false
	}
	if m.Type.Kind == ir.KindRef {
		return r.resolveAliasTarget(m.Type.RefName, visited)
	}
	return m.Type, refName, true
}

// effectiveRefNode returns the node toJsonExpr/fromJsonExpr (and their
// item-level counterparts) should dispatch on for a $ref to node.RefName.
//
// A ref straight to a real top-level object or enum model already has
// everything those need — r.kinds has a correct entry for the model's own
// name — so it's returned unchanged. Anything else is a modelKindOther
// target: a named array alias (`typedef Tags = List<Tag>;`), a named
// primitive alias (`typedef PetId = String;`), or a further $ref
// (alias-to-alias). Dart's typedef is structurally transparent — it IS the
// underlying type at compile time, not a distinct wrapper type — so a
// field of that type must be (de)serialized exactly as if it held the
// underlying concrete shape directly, not as an object with its own
// toJson()/fromJson(). This walks the alias chain via resolveAliasTarget to
// find that shape:
//
//   - a concrete primitive or array underneath is substituted in directly,
//     so toJsonExpr/fromJsonExpr's existing primitive/array branches take
//     over unchanged;
//   - a concrete object or enum reached through one or more aliases needs
//     its OWN name substituted in (not the alias's): fromJsonExpr and
//     itemFromJsonExpr call target.fromJson(...)/target.fromValue(...), and
//     only the real object/enum type has that factory/static method — a
//     typedef has none of its own to call those on;
//   - a chain that bottoms out unresolvable (an unknown or cyclic
//     reference) or at a union/intersection — itself already an
//     unsupported shape rendered as a comment-only declaration with no
//     toJson/fromJson to call — falls back to node unchanged: the prior
//     (object-shaped) dispatch this fix does not attempt to make correct
//     for a case with no clean Dart representation either way.
func (r *renderer) effectiveRefNode(node *ir.Node) *ir.Node {
	switch r.refKind(node.RefName) {
	case modelKindEnum, modelKindObject:
		return node
	default:
		resolved, resolvedName, ok := r.resolveAliasTarget(node.RefName, map[string]bool{})
		if !ok {
			return node
		}
		switch resolved.Kind {
		case ir.KindPrimitive, ir.KindArray:
			return resolved
		case ir.KindObject, ir.KindEnum:
			return &ir.Node{Kind: ir.KindRef, RefName: resolvedName}
		default:
			return node
		}
	}
}

// renderClass renders o as a class. o.Fields alone is not yet flattened
// for an allOf whose members include a $ref (see flattenFields), so this
// resolves the full field list through flattenFields rather than reading
// o.Fields directly.
func (r *renderer) renderClass(name string, o *ir.ObjectNode) (string, []string, []extraDecl) {
	fields, deps, extra, skipped := r.resolveFields(name, r.flattenFields(o))

	var sb strings.Builder
	fmt.Fprintf(&sb, "class %s {\n", name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "    final %s %s;\n", f.typeExpr, f.propertyName)
	}
	for _, wireName := range skipped {
		fmt.Fprintf(&sb, "    // %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", wireName)
	}

	sb.WriteString("\n    ")
	fmt.Fprintf(&sb, "%s({\n", name)
	for _, f := range fields {
		required := "required "
		if f.optional {
			required = ""
		}
		fmt.Fprintf(&sb, "        %sthis.%s,\n", required, f.propertyName)
	}
	sb.WriteString("    });\n")

	sb.WriteString("\n    ")
	fmt.Fprintf(&sb, "factory %s.fromJson(Map<String, dynamic> json) {\n", name)
	fmt.Fprintf(&sb, "        return %s(\n", name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "            %s: %s,\n", f.propertyName, r.fromJsonExpr(f))
	}
	sb.WriteString("        );\n")
	sb.WriteString("    }\n")

	sb.WriteString("\n    Map<String, dynamic> toJson() {\n")
	sb.WriteString("        return {\n")
	for _, f := range fields {
		fmt.Fprintf(&sb, "            %s: %s,\n", strconv.Quote(f.wireName), r.toJsonExpr(f))
	}
	sb.WriteString("        };\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String(), deps, extra
}
```

Append the following to the same file — `toJsonExpr`/`fromJsonExpr` and their array-item counterparts, consulting `r.kinds` (populated in `Generate`) to tell an object-shaped `$ref` from an enum-shaped one, mirroring `internal/gen/kotlin/generate.go`'s equivalent methods translated to Dart syntax:

```go
// toJsonExpr renders the RHS of `'<wire>': <expr>` inside toJson()'s
// returned map literal for one field. Dart's Map<String, dynamic>
// accepts any value type directly, so a primitive field needs no cast at
// all here — only a nested object/enum/array needs an explicit
// expression. Consults r.refKind, which routes through r.nameFor before
// looking up r.kinds, to tell an object-shaped $ref from an enum-shaped
// one — see refKind's doc comment for why the raw ref name can't be used
// to key r.kinds directly.
func (r *renderer) toJsonExpr(f resolvedField) string {
	switch f.node.Kind {
	case ir.KindRef:
		access := f.propertyName
		if f.optional {
			access += "?"
		}
		if r.refKind(f.node.RefName) == modelKindEnum {
			return access + ".value"
		}
		return access + ".toJson()"
	case ir.KindArray:
		access := f.propertyName
		if f.optional {
			access += "?"
		}
		return fmt.Sprintf("%s.map((e) => %s).toList()", access, r.itemToJsonExpr(f.node.Array.Items))
	default:
		return f.propertyName
	}
}

// itemToJsonExpr renders the body of the `.map((e) => ...)` closure used
// to serialize one array element. item is already the fully-resolved
// (alias-unwrapped) node for this position — resolveType's ir.KindArray
// case built it by recursing into resolveType itself, which already
// routes any ir.KindRef through effectiveRefNode — so no further
// resolution is needed here, at any nesting depth. The ir.KindArray case
// below recurses for a nested array (List<List<T>>): reusing the
// parameter name `e` at each nesting level shadows the outer one, which
// is legal Dart and never a problem here since the inner closure never
// needs to reference an outer level's element.
func (r *renderer) itemToJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindRef:
		if r.refKind(item.RefName) == modelKindEnum {
			return "e.value"
		}
		return "e.toJson()"
	case ir.KindArray:
		return fmt.Sprintf("e.map((e) => %s).toList()", r.itemToJsonExpr(item.Array.Items))
	default:
		return "e"
	}
}

// fromJsonExpr renders the RHS of `<propertyName>: <expr>` inside
// fromJson's constructor call for one field.
func (r *renderer) fromJsonExpr(f resolvedField) string {
	wire := fmt.Sprintf("json[%s]", strconv.Quote(f.wireName))
	switch f.node.Kind {
	case ir.KindPrimitive:
		return dartPrimitiveFromJsonExpr(f.node.Primitive, wire, f.optional)
	case ir.KindRef:
		target := r.nameFor(f.node.RefName)
		if r.refKind(f.node.RefName) == modelKindEnum {
			if f.optional {
				return fmt.Sprintf("%s != null ? %s.fromValue(%s as String) : null", wire, target, wire)
			}
			return fmt.Sprintf("%s.fromValue(%s as String)", target, wire)
		}
		if f.optional {
			return fmt.Sprintf("%s != null ? %s.fromJson(%s as Map<String, dynamic>) : null", wire, target, wire)
		}
		return fmt.Sprintf("%s.fromJson(%s as Map<String, dynamic>)", target, wire)
	case ir.KindArray:
		itemExpr := r.itemFromJsonExpr(f.node.Array.Items)
		if f.optional {
			return fmt.Sprintf("(%s as List<dynamic>?)?.map((e) => %s).toList()", wire, itemExpr)
		}
		return fmt.Sprintf("(%s as List<dynamic>).map((e) => %s).toList()", wire, itemExpr)
	default:
		return wire
	}
}

// dartPrimitiveFromJsonExpr casts a raw JSON value to its Dart primitive
// type. A number goes through Dart's num supertype before converting
// explicitly: a JSON number with no decimal point decodes as a Dart int,
// not double, so a direct `as double` cast fails on whole-number values.
func dartPrimitiveFromJsonExpr(p ir.Primitive, wire string, optional bool) string {
	switch p {
	case ir.PrimitiveBoolean:
		if optional {
			return wire + " as bool?"
		}
		return wire + " as bool"
	case ir.PrimitiveInteger:
		if optional {
			return wire + " as int?"
		}
		return wire + " as int"
	case ir.PrimitiveNumber:
		if optional {
			return fmt.Sprintf("(%s as num?)?.toDouble()", wire)
		}
		return fmt.Sprintf("(%s as num).toDouble()", wire)
	default: // ir.PrimitiveString and any unexpected value both read as a string
		if optional {
			return wire + " as String?"
		}
		return wire + " as String"
	}
}

// itemFromJsonExpr renders the body of the `.map((e) => ...)` closure
// used to deserialize one array element. item is already the
// fully-resolved (alias-unwrapped) node for this position, for the same
// reason itemToJsonExpr's doc comment explains. The ir.KindArray case
// recurses for a nested array (List<List<T>>): `e` at that level is raw
// dynamic (from the outer `(json[...] as List<dynamic>).map(...)`), so it
// must be cast to List<dynamic> before the inner .map(...) can run over
// it — mirroring fromJsonExpr's own top-level ir.KindArray handling one
// level down. Reusing the parameter name `e` at each nesting level is
// legal Dart (shadowing), same reasoning as itemToJsonExpr.
func (r *renderer) itemFromJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindPrimitive:
		switch item.Primitive {
		case ir.PrimitiveBoolean:
			return "e as bool"
		case ir.PrimitiveInteger:
			return "e as int"
		case ir.PrimitiveNumber:
			return "(e as num).toDouble()"
		default:
			return "e as String"
		}
	case ir.KindRef:
		target := r.nameFor(item.RefName)
		if r.refKind(item.RefName) == modelKindEnum {
			return fmt.Sprintf("%s.fromValue(e as String)", target)
		}
		return fmt.Sprintf("%s.fromJson(e as Map<String, dynamic>)", target)
	case ir.KindArray:
		return fmt.Sprintf("(e as List<dynamic>).map((e) => %s).toList()", r.itemFromJsonExpr(item.Array.Items))
	default:
		return "e"
	}
}
```

- [ ] **Step 3: Write `internal/gen/dart/generate_test.go`**

```go
package dart_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/dart"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

func buildDoc(t *testing.T, raw *spec.RawDocument) *ir.Document {
	t.Helper()
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("ir.Build: %v", err)
	}
	return doc
}

func TestGenerate_SimpleObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name":       {Type: "string"},
					"photo_urls": {Type: "array", Items: &spec.RawSchema{Type: "string"}},
				},
				Required: []string{"name", "photo_urls"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	decl := models[0].Declaration
	for _, want := range []string{
		"class Pet {",
		"final String name;",
		"final List<String> photoUrls;",
		`'photo_urls': photoUrls`,
		"factory Pet.fromJson(Map<String, dynamic> json) {",
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("declaration missing %q, got:\n%s", want, decl)
		}
	}
}

func TestGenerate_RefDependencyIsTracked(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Category": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Pet": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"category": {Ref: "#/components/schemas/Category"}},
				Required:   []string{"category"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var pet dart.ModelOutput
	for _, m := range models {
		if m.Name == "Pet" {
			pet = m
		}
	}
	if len(pet.Dependencies) != 1 || pet.Dependencies[0] != "Category" {
		t.Fatalf("expected Pet.Dependencies == [\"Category\"], got %v", pet.Dependencies)
	}
	if !strings.Contains(pet.Declaration, `Category.fromJson(json["category"] as Map<String, dynamic>)`) {
		t.Errorf("expected Category.fromJson(...) in fromJson, got:\n%s", pet.Declaration)
	}
	if !strings.Contains(pet.Declaration, "category.toJson()") {
		t.Errorf("expected category.toJson() in toJson, got:\n%s", pet.Declaration)
	}
}

func TestGenerate_AllOfFlattensRefBaseFields(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Animal": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Dog": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Animal"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"breed": {Type: "string"}}, Required: []string{"breed"}},
				},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var dogDecl string
	for _, m := range models {
		if m.Name == "Dog" {
			dogDecl = m.Declaration
		}
	}
	if dogDecl == "" {
		t.Fatal("Dog not found in output")
	}
	if !strings.Contains(dogDecl, "final String name;") {
		t.Errorf("expected Dog to have name flattened in from its $ref allOf member Animal, got:\n%s", dogDecl)
	}
	if !strings.Contains(dogDecl, "final String breed;") {
		t.Errorf("expected Dog to have its own breed field, got:\n%s", dogDecl)
	}
}

func TestGenerate_InlineEnumSynthesizesNamedTypeWithFromValue(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"status": {Type: "string", Enum: []interface{}{"available", "pending", "sold"}},
				},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 declarations, got %d: %+v", len(models), models)
	}
	var petDecl, statusDecl string
	for _, m := range models {
		switch m.Name {
		case "Pet":
			petDecl = m.Declaration
		case "PetStatus":
			statusDecl = m.Declaration
		}
	}
	if !strings.Contains(petDecl, "final PetStatus? status;") {
		t.Errorf("expected Pet.status: PetStatus?, got:\n%s", petDecl)
	}
	if !strings.Contains(statusDecl, "enum PetStatus {") {
		t.Fatalf("expected a synthesized PetStatus enum, got:\n%s", statusDecl)
	}
	for _, want := range []string{`available("available")`, `pending("pending")`, `sold("sold")`} {
		if !strings.Contains(statusDecl, want) {
			t.Errorf("PetStatus missing %q, got:\n%s", want, statusDecl)
		}
	}
}

// TestGenerate_RefKindLookupUsesResolvedNameNotRawSchemaName guards against
// the exact bug internal/gen/kotlin's own review found: r.kinds is keyed by
// the resolved Dart identifier (see Generate), but a $ref's RefName carries
// the raw IR schema name — these only agree when the schema name needs no
// sanitization. A schema named with a hyphen (a common real-world pattern)
// needs SanitizeTypeIdentifier to turn "Pet-Status" into "Pet_Status" before
// it can key into r.kinds; without routing through r.nameFor first (see
// refKind), the lookup silently misses and falls back to object-shaped
// dispatch (.toJson()/.fromJson()) for what is actually an enum.
func TestGenerate_RefKindLookupUsesResolvedNameNotRawSchemaName(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet-Status": {Type: "string", Enum: []interface{}{"available", "pending"}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"status": {Ref: "#/components/schemas/Pet-Status"},
				},
				Required: []string{"status"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl string
	for _, m := range models {
		if m.Name == "Pet" {
			petDecl = m.Declaration
		}
	}
	if petDecl == "" {
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "status.value") {
		t.Errorf("expected toJson to serialize status via .value (enum dispatch), got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "status.toJson()") {
		t.Errorf("expected status to NOT be dispatched as an object, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `Pet_Status.fromValue(json["status"] as String)`) {
		t.Errorf("expected fromJson to deserialize status via Pet_Status.fromValue, got:\n%s", petDecl)
	}
}

// TestGenerate_RefToTopLevelScalarAliasSerializesAsPrimitive guards against
// the second bug internal/gen/kotlin's review found: a $ref to a top-level
// named scalar (e.g. `PetId: {type: string}`, generated as `typedef PetId
// = String;`) must serialize as a plain String, not as an object with a
// nonexistent PetId.toJson()/PetId.fromJson() — a Dart typedef carries no
// such methods of its own, since it is not a distinct wrapper type.
func TestGenerate_RefToTopLevelScalarAliasSerializesAsPrimitive(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"PetId": {Type: "string"},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"id": {Ref: "#/components/schemas/PetId"},
				},
				Required: []string{"id"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl string
	for _, m := range models {
		if m.Name == "Pet" {
			petDecl = m.Declaration
		}
	}
	if petDecl == "" {
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "final PetId id;") {
		t.Errorf("expected Pet.id to keep the PetId alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `"id": id,`) {
		t.Errorf("expected toJson to serialize id as a plain value, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "id.toJson()") {
		t.Errorf("expected id NOT to be serialized via a nonexistent PetId.toJson(), got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `id: json["id"] as String`) {
		t.Errorf("expected fromJson to deserialize id as a plain String cast, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "PetId.fromJson") {
		t.Errorf("expected id NOT to be deserialized via a nonexistent PetId.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_RefToTopLevelArrayAliasSerializesElementwise guards against
// the same bug's other required case: a $ref to a top-level named array
// (`Categories: {type: array, items: {$ref: Category}}`, generated as
// `typedef Categories = List<Category>;`) must serialize element-wise via
// Category's own toJson()/fromJson(), not as if Categories itself were an
// object.
func TestGenerate_RefToTopLevelArrayAliasSerializesElementwise(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Category":   {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Categories": {Type: "array", Items: &spec.RawSchema{Ref: "#/components/schemas/Category"}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"categories": {Ref: "#/components/schemas/Categories"},
				},
				Required: []string{"categories"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl string
	for _, m := range models {
		if m.Name == "Pet" {
			petDecl = m.Declaration
		}
	}
	if petDecl == "" {
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "final Categories categories;") {
		t.Errorf("expected Pet.categories to keep the Categories alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `"categories": categories.map((e) => e.toJson()).toList(),`) {
		t.Errorf("expected toJson to map categories element-wise via Category.toJson(), got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "categories.toJson()") {
		t.Errorf("expected categories NOT to be serialized as if it were a single object, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `categories: (json["categories"] as List<dynamic>).map((e) => Category.fromJson(e as Map<String, dynamic>)).toList(),`) {
		t.Errorf("expected fromJson to map categories element-wise via Category.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_NestedArrayPreservesInnerTransform guards against the fourth
// bug internal/gen/kotlin's review found: an array of arrays
// (List<List<String>>) must apply the element transform at every nesting
// level, not just the outer one.
func TestGenerate_NestedArrayPreservesInnerTransform(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Matrix": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"rows": {
						Type: "array",
						Items: &spec.RawSchema{
							Type:  "array",
							Items: &spec.RawSchema{Type: "string"},
						},
					},
				},
				Required: []string{"rows"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "final List<List<String>> rows;") {
		t.Errorf("expected rows: List<List<String>>, got:\n%s", decl)
	}
	if !strings.Contains(decl, `"rows": rows.map((e) => e.map((e) => e).toList()).toList(),`) {
		t.Errorf("expected toJson to nest the .map(...) transform, got:\n%s", decl)
	}
	if !strings.Contains(decl, `rows: (json["rows"] as List<dynamic>).map((e) => (e as List<dynamic>).map((e) => e as String).toList()).toList(),`) {
		t.Errorf("expected fromJson to nest the cast-and-map transform, got:\n%s", decl)
	}
}

// TestGenerate_FlattenFields_OwnFieldOverridesBaseField guards the
// own-field-wins precedence rule: when a derived (allOf) schema redeclares
// a field its base also declares, the derived schema's own field must win
// — matching internal/gen/ts's `extends` and internal/gen/zod's
// `.extend()`, and mirroring internal/gen/swift's and internal/gen/kotlin's
// identical fix for the same gap.
func TestGenerate_FlattenFields_OwnFieldOverridesBaseField(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"DogName": {Type: "string", Enum: []interface{}{"rex", "fido"}},
			"Animal": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"name": {Type: "string"}},
				Required:   []string{"name"},
			},
			"Dog": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Animal"},
					{
						Type: "object",
						Properties: map[string]*spec.RawSchema{
							"name": {Ref: "#/components/schemas/DogName"},
						},
					},
				},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var dogDecl string
	for _, m := range models {
		if m.Name == "Dog" {
			dogDecl = m.Declaration
		}
	}
	if dogDecl == "" {
		t.Fatalf("expected a Dog declaration, got: %+v", models)
	}
	if !strings.Contains(dogDecl, "final DogName? name;") {
		t.Errorf("expected Dog.name to use the overriding DogName type, got:\n%s", dogDecl)
	}
	if strings.Contains(dogDecl, "final String name;") {
		t.Errorf("expected Dog.name to NOT fall back to the base Animal's String type, got:\n%s", dogDecl)
	}
}

func TestFileName(t *testing.T) {
	cases := map[string]string{
		"TreeNode": "tree_node.dart",
		"Pet":      "pet.dart",
		"A":        "a.dart",
	}
	for input, want := range cases {
		if got := dart.FileName(input); got != want {
			t.Errorf("FileName(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/gen/dart/... -v`
Expected: PASS.

- [ ] **Step 5: Wire `GenerateDart` into `pkg/engine`**

Open `pkg/engine/output.go`. Add `"github.com/tarikomercehajic/openapi2code/internal/gen/dart"` to the imports, then add:

```go
// DartOptions controls GenerateDart's output layout.
type DartOptions struct {
	Modular bool
}

// GenerateDart renders doc as Dart classes, laid out per opts.
//
//   - Monolithic (opts.Modular false): exactly one file, keyed
//     "generated.dart" (Dart file names are conventionally snake_case
//     even for a single combined file), holding every declaration.
//   - Modular (opts.Modular true): one file per declaration, keyed by
//     dart.FileName(model.Name) (e.g. "tree_node.dart"). Unlike
//     Swift/Kotlin, each file explicitly imports the files it depends on
//     — Dart has no implicit same-package visibility — but there is
//     still no barrel file (see Global Constraints).
func GenerateDart(doc *ir.Document, opts DartOptions) (Output, error) {
	models, err := dart.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularDartOutput(models), nil
	}
	return monolithicDartOutput(models), nil
}

func monolithicDartOutput(models []dart.ModelOutput) Output {
	var sb strings.Builder
	for _, m := range models {
		sb.WriteString(m.Declaration)
		sb.WriteString("\n")
	}
	return Output{Files: map[string]string{"generated.dart": sb.String()}}
}

func modularDartOutput(models []dart.ModelOutput) Output {
	fileNameByModel := make(map[string]string, len(models))
	for _, m := range models {
		fileNameByModel[m.Name] = dart.FileName(m.Name)
	}
	files := make(map[string]string, len(models))
	for _, m := range models {
		var content strings.Builder
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import '%s';\n", fileNameByModel[dep]))
		}
		if len(m.Dependencies) > 0 {
			content.WriteString("\n")
		}
		content.WriteString(m.Declaration)
		files[fileNameByModel[m.Name]] = content.String()
	}
	return Output{Files: files}
}
```

- [ ] **Step 6: Add Dart cases to the golden-fixture test and generate the goldens**

Follow Task 2 Step 6's exact procedure, substituting Dart: add cases calling `engine.GenerateDart(doc, engine.DartOptions{Modular: false})` for the same four fixtures, comparing `out.Files["generated.dart"]` against `testdata/golden/<fixture-basename>.dart.txt`.

Run: `go test ./pkg/engine/... -run TestGoldenFixtures -update`

**Read every generated `.dart.txt` golden file.** Confirm: `composition.yaml`'s `Dog` has flattened `name`/`breed` fields; `StringOrNumber` and the top-level anyOf `Pet` are absent; `petstore.yaml`'s `Pet` references a synthesized `PetStatus` enum also present in the same file, with lowerCamelCase enum values (`available`, `pending`, `sold`) — not `AVAILABLE` (that's Kotlin's convention, not Dart's).

As in Task 2/4 Step 6, add the modular-mode counterpart to `pkg/engine/engine_test.go` — Dart's version differs from Swift/Kotlin's in one important way: it asserts an import IS present, since Dart (unlike Swift/Kotlin) needs an explicit import between files even with no package declaration. Use a spec with a `Pet` that references a `Category` (so there's a real dependency to import), reusing whichever existing spec constant in this file already has that shape (check the TS/Zod modular tests above for one with a `$ref`-linked pair) rather than defining a new one:

```go
func TestGenerateDart_Modular_OneFilePerModelWithImportsNoBarrel(t *testing.T) {
	doc, err := engine.Parse([]byte(petCategorySpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateDart(doc, engine.DartOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateDart: %v", err)
	}
	if _, ok := out.Files["index.dart"]; ok {
		t.Error("expected no barrel file for modular Dart output")
	}
	petFile, ok := out.Files["pet.dart"]
	if !ok {
		t.Fatalf("expected pet.dart (snake_case file name), got files: %v", fileNames(out.Files))
	}
	if !strings.Contains(petFile, "import 'category.dart';") {
		t.Errorf("expected pet.dart to import category.dart, got:\n%s", petFile)
	}
	if !strings.Contains(petFile, "class Pet {") {
		t.Errorf("expected pet.dart to contain the Pet class, got:\n%s", petFile)
	}
}
```

If no existing spec constant in this file has a `Pet`-references-`Category` shape, define `petCategorySpec` as a small new one-off constant right above this test (a `Pet` object with a `category` field `$ref`-ing a `Category` object with a `name` field is enough) rather than searching further — do not reuse an unrelated fixture just to avoid adding one small constant.

- [ ] **Step 7: Run the full test suite**

Run: `go test ./pkg/engine/... -v`
Expected: all PASS.

- [ ] **Step 8: Wire `dart` into the CLI**

Repeat Task 2 Step 8's exact edits, adding `dart` as the fourth option: `usageLine` becomes `"usage: openapi2code pull <source> [--target <ts|zod|swift|kotlin|dart>] (--output <dir> | --out-file <path>)"`, the flag description becomes `` `generation target: "ts", "zod", "swift", "kotlin", or "dart"` ``, the validation check gains `&& *target != "dart"` with the matching error message, and `run.go`'s switch gains `case "dart": out, err = engine.GenerateDart(doc, engine.DartOptions{Modular: opts.OutDir != ""})`.

- [ ] **Step 9: Update the CLI tests and run the full suite**

Same as Task 2/4 Step 9: update literal `usageLine`/error-message assertions, add one end-to-end `--target dart` test to `internal/cli/run_test.go`.

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 10: Commit**

```bash
git add internal/gen/dart/ pkg/engine/output.go pkg/engine/golden_test.go pkg/engine/testdata/golden/*.dart.txt internal/cli/parse.go internal/cli/run.go internal/cli/*_test.go
git commit -m "$(cat <<'EOF'
Add the Dart generator, wired into pkg/engine and the CLI

internal/gen/dart renders plain classes with hand-written
toJson()/fromJson() (Dart has no built-in reflection-based JSON
either), lowerCamelCase enhanced enums (Dart's own convention, unlike
Kotlin's SCREAMING_SNAKE_CASE), and snake_case file names for modular
output. Dart is the one target needing explicit inter-file imports,
tracked via ModelOutput.Dependencies. --target dart is now accepted
end to end — the full PRD target list is complete.

EOF
)"
```

---

### Task 7: Dart opt-in real-compilation verification test

**Files:**
- Create: `pkg/engine/golden_dart_typecheck_test.go`

**Interfaces:**
- Consumes: the `*.dart.txt` golden files Task 6 created.
- Produces: nothing further downstream.

- [ ] **Step 1: Write the test**

Create `pkg/engine/golden_dart_typecheck_test.go`:

```go
package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoldenDartOutput_Analyzes hands every committed *.dart.txt golden
// to `dart analyze`, mirroring the Swift/Kotlin typecheck tests. Skips
// (never fails) when the dart CLI is missing or -short is set — this
// environment does not have it installed, so this test skips here today
// but runs for real wherever it is.
func TestGoldenDartOutput_Analyzes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping dart analyze check of Dart goldens in -short mode")
	}
	dartBin, err := exec.LookPath("dart")
	if err != nil {
		t.Skip("dart not found on PATH; skipping analyze check of Dart goldens (install Dart to get real compile verification)")
	}

	goldens, err := filepath.Glob(filepath.Join("testdata", "golden", "*.dart.txt"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no *.dart.txt golden files found to analyze")
	}

	// dart analyze operates on a package directory (it looks for a
	// pubspec.yaml), so each golden gets its own minimal package
	// directory rather than sharing one — this also sidesteps duplicate
	// top-level declarations across different fixtures' goldens.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, golden := range goldens {
		base := strings.TrimSuffix(filepath.Base(golden), ".txt")
		pkgDir := filepath.Join(t.TempDir(), strings.TrimSuffix(base, ".dart"))
		libDir := filepath.Join(pkgDir, "lib")
		if err := os.MkdirAll(libDir, 0o755); err != nil {
			t.Fatalf("create lib dir: %v", err)
		}
		content, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		if err := os.WriteFile(filepath.Join(libDir, base), content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		pubspec := "name: openapi2code_golden_check\nenvironment:\n  sdk: '>=2.17.0 <4.0.0'\n"
		if err := os.WriteFile(filepath.Join(pkgDir, "pubspec.yaml"), []byte(pubspec), 0o644); err != nil {
			t.Fatalf("write pubspec.yaml: %v", err)
		}

		cmd := exec.CommandContext(ctx, dartBin, "analyze", "--fatal-infos", ".")
		cmd.Dir = pkgDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("generated Dart output does not analyze cleanly (%s):\n%s", base, strings.TrimSpace(string(out)))
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./pkg/engine/... -run TestGoldenDartOutput_Analyzes -v`
Expected: SKIP in this environment (`dart` is not installed here — confirmed while designing this plan).

- [ ] **Step 3: Run the full suite once more**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add pkg/engine/golden_dart_typecheck_test.go
git commit -m "$(cat <<'EOF'
Add an opt-in dart analyze check for the Dart generator's goldens

Mirrors the Swift/Kotlin typecheck tests. Skips cleanly without the
dart CLI (not installed in this environment); runs for real wherever
it is.

EOF
)"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-09-10-future-considerations.md`
- Modify: `Claude.md`

**Interfaces:**
- Consumes: nothing new — this task documents what Tasks 1-7 built.
- Produces: nothing further downstream — this is the final task.

- [ ] **Step 1: Update `README.md`**

In the "Status" paragraph near the top, find:

```markdown
This repo currently implements **sub-project 1** (the core engine + TypeScript generator, `pkg/engine`), **sub-project 2** (the `openapi2code` CLI, `cmd/openapi2code`), **sub-project 3a** (the Zod schema generator, `--target zod`), **sub-project 4** (the WebAssembly build, `cmd/wasm`), and **sub-project 5** (the polished split-screen playground UI, `web/`). Swift, Kotlin, and Dart are a later sub-project.
```

Replace it with:

```markdown
This repo currently implements every sub-project on the original roadmap: **sub-project 1** (the core engine + TypeScript generator, `pkg/engine`), **sub-project 2** (the `openapi2code` CLI, `cmd/openapi2code`), **sub-project 3a** (the Zod schema generator, `--target zod`), **sub-project 4** (the WebAssembly build, `cmd/wasm`), **sub-project 5** (the polished split-screen playground UI, `web/`), and **sub-project 6** (the Swift/Kotlin/Dart native mobile generators, `--target swift|kotlin|dart`).
```

In the "What works today" bulleted list, find the bullet listing generation targets (it currently mentions TypeScript and Zod) and add a new bullet immediately after it:

```markdown
- Generates native mobile models: Swift `Codable` structs (classes for cyclic models), Kotlin data classes, and Dart classes — each with hand-written JSON (de)serialization requiring no consumer dependency, idiomatic camelCase field names with an explicit wire-name mapping back to the original JSON key, and a synthesized named type for any inline (non-`$ref`) enum or object a field resolves to. `oneOf`/`anyOf` and allOf's non-object-member fallback are not yet supported for these three targets (see `docs/superpowers/specs/2026-09-10-future-considerations.md`) — an affected model or field is skipped with a comment rather than generated incorrectly.
```

Find the CLI usage section's `--target` documentation (the line describing what values `--target` accepts) and update it to mention all five targets: `ts` (default), `zod`, `swift`, `kotlin`, `dart`.

Find the "Library usage" section's description of `pkg/engine`'s public API ("The whole public API is ... functions") and update the function count/list to include `GenerateSwift`, `GenerateKotlin`, `GenerateDart` alongside `Parse`, `GenerateTS`, `GenerateZod`.

Find the "Project layout" section's tree and add:

```
internal/gen/mobile/  Naming helpers shared by the Swift/Kotlin/Dart generators
internal/gen/swift/   Swift code generation from the IR
internal/gen/kotlin/  Kotlin code generation from the IR
internal/gen/dart/    Dart code generation from the IR
```

right after the existing `internal/gen/zod/` line.

- [ ] **Step 2: Add a deferred-scope entry to `future-considerations.md`**

Append a new section at the end of `docs/superpowers/specs/2026-09-10-future-considerations.md`:

```markdown
## oneOf/anyOf support for Swift/Kotlin/Dart

**Status:** explicitly out of scope for sub-project 6, not just
unimplemented. A model or field using `oneOf`/`anyOf` (or allOf's
non-object-member intersection fallback) is skipped from Swift/Kotlin/
Dart output with a comment, rather than generated incorrectly — see
`docs/superpowers/specs/2026-09-11-mobile-generators-design.md`.

**Why it's deferred:** none of the three target languages has a
construct as directly expressible as TS's `A | B` or Zod's
`z.union([...])`. Supporting it idiomatically needs a real per-language
design: an enum-with-associated-values in Swift, a sealed class
hierarchy in Kotlin/Dart, plus hand-written "try each variant" decode
logic for each — three more per-language design-and-implementation
efforts, layered onto everything else sub-project 6 already covers.

**Why it's worth revisiting:** it's the one remaining feature-parity gap
between the mobile generators and TS/Zod. `pkg/engine/testdata/composition.yaml`
already exercises this fixture shape (`StringOrNumber`, and `Pet` via
`anyOf`) for TS/Zod but both are silently absent from the Swift/Kotlin/
Dart golden output for that same fixture — worth being aware of before
treating the mobile generators as fully feature-complete.

## Comma-separated multi-target CLI support

**Status:** documented in `Claude.md`'s architecture notes since this
project's first commit, never implemented — not even for the original
`ts`/`zod` pair, let alone the five targets now available after
sub-project 6. `--target` accepts exactly one value per invocation.

**Why it's worth revisiting:** with five targets now available, running
the CLI five times (once per target) to regenerate everything for a spec
is more friction than it needs to be. A `--target ts,zod,swift` shape
would need: comma-splitting and validating each value, running
generation once per target, and deciding how modular output paths avoid
colliding across targets when writing into one `--output` directory
(e.g. a per-target subdirectory). None of this touches `pkg/engine` — it's
entirely an `internal/cli` change.
```

- [ ] **Step 3: Update `Claude.md`'s status line**

Change:

```markdown
Sub-projects 1 (core engine + TypeScript generator, `pkg/engine`), 2 (CLI, `cmd/openapi2code` + `internal/cli`), 3a (Zod generator, `--target zod`), 4 (WASM build, `internal/wasmapi` + `cmd/wasm`), and 5 (the browser playground, `web/`) are complete. Only Swift, Kotlin, and Dart generators remain from the original roadmap.
```

to:

```markdown
Every sub-project on the original roadmap is complete: 1 (core engine + TypeScript generator, `pkg/engine`), 2 (CLI, `cmd/openapi2code` + `internal/cli`), 3a (Zod generator, `--target zod`), 4 (WASM build, `internal/wasmapi` + `cmd/wasm`), 5 (the browser playground, `web/`), and 6 (Swift/Kotlin/Dart generators, `internal/gen/swift`, `internal/gen/kotlin`, `internal/gen/dart`). oneOf/anyOf support for the mobile targets and comma-separated multi-target CLI support remain as deferred follow-ups — see `docs/superpowers/specs/2026-09-10-future-considerations.md`.
```

- [ ] **Step 4: Run the complete verification suite**

```bash
go build ./...
go vet ./...
go test ./... -count=1
./web/test.sh
```

Expected: every Go package `ok`, `./web/test.sh` ends with `All WASM checks passed.` (the web playground doesn't gain the new targets — that's explicitly out of scope, per the design spec — so nothing there should need to change).

- [ ] **Step 5: Commit**

```bash
git add README.md docs/superpowers/specs/2026-09-10-future-considerations.md Claude.md
git commit -m "$(cat <<'EOF'
Update documentation for the Swift/Kotlin/Dart generators

Records oneOf/anyOf-for-mobile and comma-separated multi-target CLI
support as deferred follow-ups now that every sub-project on the
original roadmap is complete.

EOF
)"
```
