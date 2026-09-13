# Core Engine + IR + TypeScript Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go module that parses OpenAPI/Swagger v2 and v3 specs (JSON or YAML) into an intermediate representation (IR), and generates TypeScript interface declarations from that IR, in both modular (multi-file + barrel `index.ts`) and monolithic (single-file) output modes.

**Architecture:** Three layered internal packages (`internal/spec` → `internal/ir` → `internal/gen/ts`) plus one public orchestration package (`pkg/engine`). Each layer only knows about the layer below it; `pkg/engine.Parse([]byte)` and `pkg/engine.GenerateTS(...)` are the only public entry points.

**Tech Stack:** Go (standard library), `gopkg.in/yaml.v3` (the one justified third-party dependency, for YAML input).

**Spec:** `docs/superpowers/specs/2026-09-10-core-engine-ts-generator-design.md`

## Global Constraints

- Go module path: `github.com/tarikomercehajic/openapi2code`.
- The core engine (`internal/spec`, `internal/ir`, `internal/gen/ts`, `pkg/engine`) does no file I/O or network access — `Parse` takes `[]byte` only, so it stays portable to a future WASM build unchanged.
- No third-party dependencies beyond `gopkg.in/yaml.v3` (justified: no YAML parser exists in the Go standard library, and YAML input is required).
- `$ref` resolution covers only internal refs: `#/components/schemas/*` (v3) and `#/definitions/*` (v2). External file/URL refs are unsupported and are not an error to detect specially — they simply fail to resolve.
- Parsing is lenient: unknown/unsupported fields are ignored, not errors. Hard errors are reserved for malformed JSON/YAML, unresolvable `$ref`s, and constructs that cannot be modeled.
- Errors are returned, never panicked, and wrapped with a path through the spec (schema name, field name, `allOf[i]`, etc.) so failures are traceable.
- Enums render as TypeScript string-literal unions (`"a" | "b"`), never `enum` declarations.
- Nullable (`nullable`/`x-nullable` → `T | null`) and optional (`required[]` membership → `field?:`) are tracked and rendered as distinct concepts — a field can be both.
- `allOf`, `oneOf`, and `anyOf` are fully supported: `allOf` renders as TS interface extension when every member is object-shaped, falling back to an intersection type (`A & B`) when any member isn't. `oneOf`/`anyOf` render as TS union types.
- Circular schema references must not cause infinite recursion, and the IR must record which models participate in a cycle (`Model.Cyclic`), even though this sub-project's TS generator doesn't need to act on that flag — a future Zod generator will.
- Paths/operations parsing is out of scope. Only `components/schemas` (v3) / `definitions` (v2) are read; everything else in the document is ignored by lenient unmarshaling.

---

## Task 1: Module scaffold + spec version detection

**Files:**
- Create: `go.mod`
- Create: `internal/spec/version.go`
- Test: `internal/spec/version_test.go`

**Interfaces:**
- Produces: `spec.Version` (int enum: `VersionUnknown`, `VersionV2`, `VersionV3`), `spec.DetectVersion(jsonData []byte) (Version, error)`, unexported `normalizeToJSON(data []byte) ([]byte, error)`.

- [ ] **Step 1: Scaffold the Go module**

```bash
mkdir -p /Users/tarikomercehajic/code/openapi2code/internal/spec
cd /Users/tarikomercehajic/code/openapi2code
go mod init github.com/tarikomercehajic/openapi2code
go get gopkg.in/yaml.v3@v3.0.1
```

- [ ] **Step 2: Write the failing test**

Create `internal/spec/version_test.go`:

```go
package spec

import "testing"

func TestNormalizeToJSON_PassesThroughJSON(t *testing.T) {
	input := []byte(`{"openapi":"3.0.3"}`)
	got, err := normalizeToJSON(input)
	if err != nil {
		t.Fatalf("normalizeToJSON: %v", err)
	}
	if string(got) != string(input) {
		t.Errorf("got %s, want %s", got, input)
	}
}

func TestNormalizeToJSON_ConvertsYAML(t *testing.T) {
	input := []byte("openapi: 3.0.3\ninfo:\n  title: Test\n")
	got, err := normalizeToJSON(input)
	if err != nil {
		t.Fatalf("normalizeToJSON: %v", err)
	}
	version, err := DetectVersion(got)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got version %v, want VersionV3", version)
	}
}

func TestDetectVersion_V3(t *testing.T) {
	version, err := DetectVersion([]byte(`{"openapi":"3.0.3"}`))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got %v, want VersionV3", version)
	}
}

func TestDetectVersion_V2(t *testing.T) {
	version, err := DetectVersion([]byte(`{"swagger":"2.0"}`))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV2 {
		t.Errorf("got %v, want VersionV2", version)
	}
}

func TestDetectVersion_Unknown(t *testing.T) {
	_, err := DetectVersion([]byte(`{"foo":"bar"}`))
	if err == nil {
		t.Fatal("expected error for unrecognized document, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/spec/...`
Expected: FAIL — `normalizeToJSON`, `DetectVersion`, `VersionV2`, `VersionV3`, `VersionUnknown` undefined.

- [ ] **Step 4: Write the implementation**

Create `internal/spec/version.go`:

```go
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version identifies which OpenAPI/Swagger major version a document uses.
type Version int

const (
	VersionUnknown Version = iota
	VersionV2
	VersionV3
)

// normalizeToJSON returns data as JSON bytes. If data is already JSON
// (starts with '{' or '['), it is returned unchanged; otherwise it is
// parsed as YAML and re-encoded as JSON so the rest of the package only
// ever deals with one format.
func normalizeToJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return data, nil
	}
	var generic interface{}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, fmt.Errorf("parse spec as YAML: %w", err)
	}
	jsonData, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("convert parsed YAML to JSON: %w", err)
	}
	return jsonData, nil
}

type versionProbe struct {
	Swagger string `json:"swagger"`
	OpenAPI string `json:"openapi"`
}

// DetectVersion inspects the top-level "openapi" or "swagger" field of a
// normalized JSON document to determine which spec version it uses.
func DetectVersion(jsonData []byte) (Version, error) {
	var probe versionProbe
	if err := json.Unmarshal(jsonData, &probe); err != nil {
		return VersionUnknown, fmt.Errorf("detect spec version: %w", err)
	}
	switch {
	case strings.HasPrefix(probe.OpenAPI, "3."):
		return VersionV3, nil
	case probe.Swagger == "2.0":
		return VersionV2, nil
	default:
		return VersionUnknown, fmt.Errorf("detect spec version: missing or unrecognized 'openapi'/'swagger' field")
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/spec/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/spec/version.go internal/spec/version_test.go
git commit -m "feat: scaffold module and add spec version detection"
```

---

## Task 2: Raw schema types and parsing (v2 + v3)

**Files:**
- Create: `internal/spec/schema.go`
- Test: `internal/spec/schema_test.go`

**Interfaces:**
- Consumes: `spec.Version`, `spec.VersionV2`, `spec.VersionV3` (Task 1).
- Produces: `spec.RawSchema` struct (fields: `Ref`, `Type`, `Properties map[string]*RawSchema`, `Required []string`, `Items *RawSchema`, `Enum []interface{}`, `Nullable bool`, `XNullable bool`, `AllOf`/`OneOf`/`AnyOf []*RawSchema`), `(*RawSchema) IsNullable() bool`, `spec.RawDocument{Version Version, Schemas map[string]*RawSchema}`, `spec.ParseRaw(jsonData []byte, version Version) (*RawDocument, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/spec/schema_test.go`:

```go
package spec

import "testing"

func TestParseRaw_V3(t *testing.T) {
	doc, err := ParseRaw([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Pet": {
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}
			}
		}
	}`), VersionV3)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	pet, ok := doc.Schemas["Pet"]
	if !ok {
		t.Fatal("expected schema 'Pet'")
	}
	if pet.Type != "object" {
		t.Errorf("got type %q, want object", pet.Type)
	}
	if len(pet.Properties) != 1 || pet.Properties["name"].Type != "string" {
		t.Errorf("unexpected properties: %+v", pet.Properties)
	}
	if len(pet.Required) != 1 || pet.Required[0] != "name" {
		t.Errorf("unexpected required: %v", pet.Required)
	}
}

func TestParseRaw_V2(t *testing.T) {
	doc, err := ParseRaw([]byte(`{
		"swagger": "2.0",
		"definitions": {
			"Pet": {
				"type": "object",
				"properties": {
					"name": {"type": "string", "x-nullable": true}
				}
			}
		}
	}`), VersionV2)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	pet, ok := doc.Schemas["Pet"]
	if !ok {
		t.Fatal("expected schema 'Pet'")
	}
	if !pet.Properties["name"].IsNullable() {
		t.Error("expected 'name' to be nullable via x-nullable")
	}
}

func TestIsNullable_V3Field(t *testing.T) {
	s := &RawSchema{Nullable: true}
	if !s.IsNullable() {
		t.Error("expected IsNullable() to be true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spec/...`
Expected: FAIL — `RawSchema`, `ParseRaw` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/spec/schema.go`:

```go
package spec

import (
	"encoding/json"
	"fmt"
)

// RawSchema is the raw, version-agnostic shape of an OpenAPI v2/v3 schema
// object, unmarshaled directly from JSON before any $ref resolution or
// IR construction happens.
type RawSchema struct {
	Ref        string                 `json:"$ref,omitempty"`
	Type       string                 `json:"type,omitempty"`
	Properties map[string]*RawSchema  `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
	Items      *RawSchema             `json:"items,omitempty"`
	Enum       []interface{}          `json:"enum,omitempty"`
	Nullable   bool                   `json:"nullable,omitempty"`
	XNullable  bool                   `json:"x-nullable,omitempty"`
	AllOf      []*RawSchema           `json:"allOf,omitempty"`
	OneOf      []*RawSchema           `json:"oneOf,omitempty"`
	AnyOf      []*RawSchema           `json:"anyOf,omitempty"`
}

// IsNullable reports whether the schema is nullable under either the v3
// "nullable" keyword or the v2 "x-nullable" vendor extension.
func (s *RawSchema) IsNullable() bool {
	return s.Nullable || s.XNullable
}

// RawDocument is the version-normalized set of named schemas from an
// OpenAPI/Swagger document: v3's components.schemas and v2's definitions
// are both flattened into the same Schemas map, keyed by schema name.
type RawDocument struct {
	Version Version
	Schemas map[string]*RawSchema
}

type rawDocV3 struct {
	Components struct {
		Schemas map[string]*RawSchema `json:"schemas"`
	} `json:"components"`
}

type rawDocV2 struct {
	Definitions map[string]*RawSchema `json:"definitions"`
}

// ParseRaw unmarshals a normalized JSON document of the given version into
// a RawDocument. It does not resolve or validate $refs.
func ParseRaw(jsonData []byte, version Version) (*RawDocument, error) {
	switch version {
	case VersionV3:
		var doc rawDocV3
		if err := json.Unmarshal(jsonData, &doc); err != nil {
			return nil, fmt.Errorf("parse v3 spec: %w", err)
		}
		return &RawDocument{Version: version, Schemas: doc.Components.Schemas}, nil
	case VersionV2:
		var doc rawDocV2
		if err := json.Unmarshal(jsonData, &doc); err != nil {
			return nil, fmt.Errorf("parse v2 spec: %w", err)
		}
		return &RawDocument{Version: version, Schemas: doc.Definitions}, nil
	default:
		return nil, fmt.Errorf("parse spec: unsupported version")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spec/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/spec/schema.go internal/spec/schema_test.go
git commit -m "feat: parse raw v2/v3 schemas into a version-normalized document"
```

---

## Task 3: Internal $ref resolution + top-level spec.Parse

**Files:**
- Create: `internal/spec/refs.go`
- Test: `internal/spec/refs_test.go`

**Interfaces:**
- Consumes: `spec.RawDocument`, `spec.RawSchema`, `spec.normalizeToJSON`, `spec.DetectVersion`, `spec.ParseRaw` (Tasks 1-2).
- Produces: `(*RawDocument) ResolveRefName(ref string) (string, error)`, `spec.Parse(data []byte) (*RawDocument, error)` — the single top-level entry point that normalizes, detects version, raw-parses, and validates every `$ref` resolves.

- [ ] **Step 1: Write the failing test**

Create `internal/spec/refs_test.go`:

```go
package spec

import "testing"

func TestResolveRefName_V3(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	name, err := doc.ResolveRefName("#/components/schemas/Pet")
	if err != nil {
		t.Fatalf("ResolveRefName: %v", err)
	}
	if name != "Pet" {
		t.Errorf("got %q, want Pet", name)
	}
}

func TestResolveRefName_V2(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	name, err := doc.ResolveRefName("#/definitions/Pet")
	if err != nil {
		t.Fatalf("ResolveRefName: %v", err)
	}
	if name != "Pet" {
		t.Errorf("got %q, want Pet", name)
	}
}

func TestResolveRefName_Unresolvable(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	if _, err := doc.ResolveRefName("#/components/schemas/Missing"); err == nil {
		t.Fatal("expected error for unresolvable ref, got nil")
	}
}

func TestResolveRefName_External(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{}}
	if _, err := doc.ResolveRefName("other.yaml#/Pet"); err == nil {
		t.Fatal("expected error for external ref, got nil")
	}
}

func TestParse_ValidatesRefs(t *testing.T) {
	_, err := Parse([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Pet": {
					"type": "object",
					"properties": {
						"category": {"$ref": "#/components/schemas/Missing"}
					}
				}
			}
		}
	}`))
	if err == nil {
		t.Fatal("expected error for unresolvable ref, got nil")
	}
}

func TestParse_Success(t *testing.T) {
	doc, err := Parse([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Category": {"type": "object"},
				"Pet": {
					"type": "object",
					"properties": {
						"category": {"$ref": "#/components/schemas/Category"}
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(doc.Schemas))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/spec/...`
Expected: FAIL — `ResolveRefName`, `Parse` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/spec/refs.go`:

```go
package spec

import (
	"fmt"
	"strings"
)

const (
	refPrefixV2 = "#/definitions/"
	refPrefixV3 = "#/components/schemas/"
)

// ResolveRefName extracts the schema name from an internal $ref string
// (e.g. "#/components/schemas/Pet" or "#/definitions/Pet" -> "Pet") and
// verifies it exists in the document. External or otherwise-shaped refs
// are rejected: only internal refs are supported.
func (d *RawDocument) ResolveRefName(ref string) (string, error) {
	var name string
	switch {
	case strings.HasPrefix(ref, refPrefixV2):
		name = strings.TrimPrefix(ref, refPrefixV2)
	case strings.HasPrefix(ref, refPrefixV3):
		name = strings.TrimPrefix(ref, refPrefixV3)
	default:
		return "", fmt.Errorf("resolve ref %q: unsupported or external ref (only internal #/definitions/* and #/components/schemas/* refs are supported)", ref)
	}
	if _, ok := d.Schemas[name]; !ok {
		return "", fmt.Errorf("resolve ref %q: schema %q not found", ref, name)
	}
	return name, nil
}

func (d *RawDocument) validateRefs() error {
	for name, s := range d.Schemas {
		if err := validateSchemaRefs(d, s); err != nil {
			return fmt.Errorf("schema %q: %w", name, err)
		}
	}
	return nil
}

func validateSchemaRefs(d *RawDocument, s *RawSchema) error {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		_, err := d.ResolveRefName(s.Ref)
		return err
	}
	for name, p := range s.Properties {
		if err := validateSchemaRefs(d, p); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	if s.Items != nil {
		if err := validateSchemaRefs(d, s.Items); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	for i, sub := range s.AllOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("allOf[%d]: %w", i, err)
		}
	}
	for i, sub := range s.OneOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("oneOf[%d]: %w", i, err)
		}
	}
	for i, sub := range s.AnyOf {
		if err := validateSchemaRefs(d, sub); err != nil {
			return fmt.Errorf("anyOf[%d]: %w", i, err)
		}
	}
	return nil
}

// Parse normalizes (YAML or JSON), version-detects, raw-parses, and
// validates every $ref in an OpenAPI/Swagger document. It is the single
// entry point internal/ir and pkg/engine use to go from raw bytes to a
// validated RawDocument.
func Parse(data []byte) (*RawDocument, error) {
	jsonData, err := normalizeToJSON(data)
	if err != nil {
		return nil, err
	}
	version, err := DetectVersion(jsonData)
	if err != nil {
		return nil, err
	}
	doc, err := ParseRaw(jsonData, version)
	if err != nil {
		return nil, err
	}
	if err := doc.validateRefs(); err != nil {
		return nil, err
	}
	return doc, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/spec/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/spec/refs.go internal/spec/refs_test.go
git commit -m "feat: resolve and validate internal \$refs, add spec.Parse entry point"
```

---

## Task 4: IR core types + builder (objects, primitives, refs, cycle detection)

**Files:**
- Create: `internal/ir/types.go`
- Create: `internal/ir/build.go`
- Test: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `spec.RawDocument`, `spec.RawSchema`, `(*RawDocument) ResolveRefName` (Tasks 2-3).
- Produces: full IR type vocabulary — `ir.Kind` (`KindPrimitive`, `KindObject`, `KindArray`, `KindEnum`, `KindUnion`, `KindIntersection`, `KindRef`), `ir.Primitive` (`PrimitiveString`, `PrimitiveNumber`, `PrimitiveInteger`, `PrimitiveBoolean`, `PrimitiveUnknown`), `ir.Node{Kind, Primitive, Object *ObjectNode, Array *ArrayNode, Enum *EnumNode, Union *UnionNode, Intersection *IntersectionNode, RefName string}`, `ir.Field{Name string, Type *Node, Optional bool, Nullable bool}`, `ir.ObjectNode{Extends []string, Fields []*Field}`, `ir.ArrayNode{Items *Node}`, `ir.EnumNode{Values []string}`, `ir.UnionNode{Variants []*Node}`, `ir.IntersectionNode{Members []*Node}`, `ir.Model{Name string, Type *Node, Cyclic bool}`, `ir.Document{Models []*Model}`, `ir.Build(raw *spec.RawDocument) (*Document, error)`.
  - This task implements only the `KindRef`, `KindObject`, and `KindPrimitive` branches of the builder plus cycle detection; `KindEnum`/`KindArray`/`KindUnion`/`KindIntersection` construction land in Tasks 5-8, but their types are defined now so cycle detection's full switch compiles against the complete vocabulary from the start.

- [ ] **Step 1: Write the failing test**

Create `internal/ir/build_test.go`:

```go
package ir_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

func TestBuild_SimpleObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name": {Type: "string"},
					"age":  {Type: "integer"},
				},
				Required: []string{"name"},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(doc.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(doc.Models))
	}
	m := doc.Models[0]
	if m.Name != "Pet" || m.Type.Kind != ir.KindObject {
		t.Fatalf("unexpected model: %+v", m)
	}
	fields := m.Type.Object.Fields
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	// Fields are sorted alphabetically by name: age, name.
	if fields[0].Name != "age" || !fields[0].Optional {
		t.Errorf("expected optional 'age' first, got %+v", fields[0])
	}
	if fields[1].Name != "name" || fields[1].Optional {
		t.Errorf("expected required 'name' second, got %+v", fields[1])
	}
}

func TestBuild_RefFieldNotInlined(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Category": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"category": {Ref: "#/components/schemas/Category"},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	pet := modelByName(t, doc, "Pet")
	field := pet.Type.Object.Fields[0]
	if field.Type.Kind != ir.KindRef || field.Type.RefName != "Category" {
		t.Errorf("expected Ref(Category), got %+v", field.Type)
	}
}

func TestBuild_UnresolvableRefErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"category": {Ref: "#/components/schemas/Missing"},
				},
			},
		},
	}
	if _, err := ir.Build(raw); err == nil {
		t.Fatal("expected error for unresolvable ref, got nil")
	}
}

func TestBuild_SelfCycleMarked(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Node": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"next": {Ref: "#/components/schemas/Node"},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !doc.Models[0].Cyclic {
		t.Error("expected Node to be marked Cyclic")
	}
}

func TestBuild_MutualCycleMarked(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"A": {Type: "object", Properties: map[string]*spec.RawSchema{"b": {Ref: "#/components/schemas/B"}}},
			"B": {Type: "object", Properties: map[string]*spec.RawSchema{"a": {Ref: "#/components/schemas/A"}}},
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

func TestBuild_NonCyclicNotMarked(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Category": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}},
			"Pet":      {Type: "object", Properties: map[string]*spec.RawSchema{"category": {Ref: "#/components/schemas/Category"}}},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, m := range doc.Models {
		if m.Cyclic {
			t.Errorf("expected %s to not be marked Cyclic", m.Name)
		}
	}
}

func modelByName(t *testing.T, doc *ir.Document, name string) *ir.Model {
	t.Helper()
	for _, m := range doc.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("model %q not found", name)
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — package `ir` does not exist / undefined symbols.

- [ ] **Step 3: Write the implementation**

Create `internal/ir/types.go`:

```go
package ir

// Kind identifies which shape an IR Node has.
type Kind int

const (
	KindPrimitive Kind = iota
	KindObject
	KindArray
	KindEnum
	KindUnion
	KindIntersection
	KindRef
)

// Primitive identifies a scalar JSON Schema type.
type Primitive int

const (
	PrimitiveString Primitive = iota
	PrimitiveNumber
	PrimitiveInteger
	PrimitiveBoolean
	PrimitiveUnknown
)

// Node is a single type-shaped node in the IR. Exactly one of Object,
// Array, Enum, Union, Intersection, or RefName is meaningful, selected by
// Kind; Primitive is meaningful only when Kind == KindPrimitive.
type Node struct {
	Kind         Kind
	Primitive    Primitive
	Object       *ObjectNode
	Array        *ArrayNode
	Enum         *EnumNode
	Union        *UnionNode
	Intersection *IntersectionNode
	RefName      string
}

// Field is a named, typed member of an ObjectNode.
type Field struct {
	Name     string
	Type     *Node
	Optional bool
	Nullable bool
}

// ObjectNode is a struct-shaped type. Extends names other top-level
// models this object should be rendered as extending (populated when the
// object came from an allOf composed entirely of object-shaped members).
type ObjectNode struct {
	Extends []string
	Fields  []*Field
}

// ArrayNode is a homogeneous list type.
type ArrayNode struct {
	Items *Node
}

// EnumNode is a closed set of literal string values.
type EnumNode struct {
	Values []string
}

// UnionNode is a set of alternative types (from oneOf/anyOf).
type UnionNode struct {
	Variants []*Node
}

// IntersectionNode is a set of types that must all be satisfied at once —
// the fallback rendering for an allOf that isn't entirely object-shaped.
type IntersectionNode struct {
	Members []*Node
}

// Model is one top-level named schema.
type Model struct {
	Name   string
	Type   *Node
	Cyclic bool
}

// Document is the fully-built IR for a parsed spec: every top-level
// schema, in deterministic (name-sorted) order.
type Document struct {
	Models []*Model
}
```

Create `internal/ir/build.go`:

```go
package ir

import (
	"fmt"
	"sort"

	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

// Build walks a validated RawDocument and produces the IR Document for it.
func Build(raw *spec.RawDocument) (*Document, error) {
	names := make([]string, 0, len(raw.Schemas))
	for name := range raw.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	models := make([]*Model, 0, len(names))
	for _, name := range names {
		node, err := buildNode(raw, raw.Schemas[name])
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		models = append(models, &Model{Name: name, Type: node})
	}

	doc := &Document{Models: models}
	markCycles(doc)
	return doc, nil
}

func buildNode(raw *spec.RawDocument, s *spec.RawSchema) (*Node, error) {
	if s.Ref != "" {
		name, err := raw.ResolveRefName(s.Ref)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KindRef, RefName: name}, nil
	}
	switch s.Type {
	case "string":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveString}, nil
	case "number":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveNumber}, nil
	case "integer":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveInteger}, nil
	case "boolean":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveBoolean}, nil
	case "object", "":
		return buildObjectNode(raw, s)
	default:
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}, nil
	}
}

func buildObjectNode(raw *spec.RawDocument, s *spec.RawSchema) (*Node, error) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]*Field, 0, len(names))
	for _, name := range names {
		propSchema := s.Properties[name]
		fieldType, err := buildNode(raw, propSchema)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, &Field{
			Name:     name,
			Type:     fieldType,
			Optional: !requiredSet[name],
			Nullable: propSchema.IsNullable(),
		})
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Fields: fields}}, nil
}

// markCycles sets Cyclic on every model that is reachable from itself by
// following Ref edges through other models' type trees.
func markCycles(doc *Document) {
	byName := make(map[string]*Model, len(doc.Models))
	for _, m := range doc.Models {
		byName[m.Name] = m
	}
	for _, m := range doc.Models {
		if reachesSelf(m.Name, m.Type, byName, map[string]bool{}) {
			m.Cyclic = true
		}
	}
}

func reachesSelf(start string, node *Node, byName map[string]*Model, visiting map[string]bool) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case KindRef:
		if node.RefName == start {
			return true
		}
		if visiting[node.RefName] {
			return false
		}
		target, ok := byName[node.RefName]
		if !ok {
			return false
		}
		visiting[node.RefName] = true
		defer delete(visiting, node.RefName)
		return reachesSelf(start, target.Type, byName, visiting)
	case KindObject:
		for _, f := range node.Object.Fields {
			if reachesSelf(start, f.Type, byName, visiting) {
				return true
			}
		}
	case KindArray:
		return reachesSelf(start, node.Array.Items, byName, visiting)
	case KindUnion:
		for _, v := range node.Union.Variants {
			if reachesSelf(start, v, byName, visiting) {
				return true
			}
		}
	case KindIntersection:
		for _, member := range node.Intersection.Members {
			if reachesSelf(start, member, byName, visiting) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/types.go internal/ir/build.go internal/ir/build_test.go
git commit -m "feat: add IR types and builder for objects, primitives, refs, and cycle detection"
```

---

## Task 5: IR builder — enums

**Files:**
- Modify: `internal/ir/build.go`
- Modify: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `ir.KindEnum`, `ir.EnumNode` (Task 4).
- Produces: `buildNode` now handles schemas with a non-empty `Enum` field.

- [ ] **Step 1: Write the failing test**

Add to `internal/ir/build_test.go`:

```go
func TestBuild_Enum(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Status": {Type: "string", Enum: []interface{}{"active", "inactive"}},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindEnum {
		t.Fatalf("expected KindEnum, got %v", node.Kind)
	}
	if len(node.Enum.Values) != 2 || node.Enum.Values[0] != "active" || node.Enum.Values[1] != "inactive" {
		t.Errorf("got %v, want [active inactive]", node.Enum.Values)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — `TestBuild_Enum` fails because enum schemas currently fall through to the `"string"` primitive case (a `type: "string"` schema with `Enum` set is built as a plain string, not an enum).

- [ ] **Step 3: Write the implementation**

In `internal/ir/build.go`, update `buildNode` to check for `Enum` before the type switch:

```go
func buildNode(raw *spec.RawDocument, s *spec.RawSchema) (*Node, error) {
	if s.Ref != "" {
		name, err := raw.ResolveRefName(s.Ref)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KindRef, RefName: name}, nil
	}
	if len(s.Enum) > 0 {
		return buildEnumNode(s), nil
	}
	switch s.Type {
	case "string":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveString}, nil
	case "number":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveNumber}, nil
	case "integer":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveInteger}, nil
	case "boolean":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveBoolean}, nil
	case "object", "":
		return buildObjectNode(raw, s)
	default:
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}, nil
	}
}

// buildEnumNode stringifies enum values generically. Only string-valued
// enums (the overwhelmingly common real-world case) are rendered
// correctly as TS string-literal unions; numeric/boolean enum values are
// stringified as-is rather than given type-specific literal formatting.
func buildEnumNode(s *spec.RawSchema) *Node {
	values := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		values = append(values, fmt.Sprint(v))
	}
	return &Node{Kind: KindEnum, Enum: &EnumNode{Values: values}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go
git commit -m "feat: build enum schemas into IR EnumNode"
```

---

## Task 6: IR builder — arrays

**Files:**
- Modify: `internal/ir/build.go`
- Modify: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `ir.KindArray`, `ir.ArrayNode` (Task 4).
- Produces: `buildNode` now handles `type: "array"` schemas.

- [ ] **Step 1: Write the failing test**

Add to `internal/ir/build_test.go`:

```go
func TestBuild_Array(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Tags": {Type: "array", Items: &spec.RawSchema{Type: "string"}},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindArray {
		t.Fatalf("expected KindArray, got %v", node.Kind)
	}
	if node.Array.Items.Kind != ir.KindPrimitive || node.Array.Items.Primitive != ir.PrimitiveString {
		t.Errorf("expected string item type, got %+v", node.Array.Items)
	}
}

func TestBuild_ArrayOfSelfCreatesCycle(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"TreeNode": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"children": {Type: "array", Items: &spec.RawSchema{Ref: "#/components/schemas/TreeNode"}},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !doc.Models[0].Cyclic {
		t.Error("expected TreeNode to be marked Cyclic via array item ref")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — `type: "array"` schemas currently fall to the `default` case and build as `PrimitiveUnknown`, so `node.Kind != ir.KindArray`.

- [ ] **Step 3: Write the implementation**

In `internal/ir/build.go`, add an `"array"` case to `buildNode`'s type switch (before `"object", ""`):

```go
	switch s.Type {
	case "array":
		return buildArrayNode(raw, s)
	case "string":
```

Add the new function:

```go
func buildArrayNode(raw *spec.RawDocument, s *spec.RawSchema) (*Node, error) {
	if s.Items == nil {
		return &Node{Kind: KindArray, Array: &ArrayNode{Items: &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}}}, nil
	}
	items, err := buildNode(raw, s.Items)
	if err != nil {
		return nil, fmt.Errorf("items: %w", err)
	}
	return &Node{Kind: KindArray, Array: &ArrayNode{Items: items}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go
git commit -m "feat: build array schemas into IR ArrayNode"
```

---

## Task 7: IR builder — allOf (extends / intersection fallback)

**Files:**
- Modify: `internal/ir/build.go`
- Modify: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `ir.KindIntersection`, `ir.IntersectionNode`, `ir.ObjectNode.Extends` (Task 4).
- Produces: `buildNode` now handles non-empty `AllOf`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ir/build_test.go`:

```go
func TestBuild_AllOfExtends(t *testing.T) {
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
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dog := modelByName(t, doc, "Dog")
	if dog.Type.Kind != ir.KindObject {
		t.Fatalf("expected KindObject, got %v", dog.Type.Kind)
	}
	if len(dog.Type.Object.Extends) != 1 || dog.Type.Object.Extends[0] != "Animal" {
		t.Errorf("expected Extends [Animal], got %v", dog.Type.Object.Extends)
	}
	if len(dog.Type.Object.Fields) != 1 || dog.Type.Object.Fields[0].Name != "breed" {
		t.Errorf("expected [breed] field, got %v", dog.Type.Object.Fields)
	}
}

func TestBuild_AllOfWithNonObjectMemberFallsBackToIntersection(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Base": {Type: "object", Properties: map[string]*spec.RawSchema{"id": {Type: "string"}}},
			"Weird": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Base"},
					{Type: "string"},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	weird := modelByName(t, doc, "Weird")
	if weird.Type.Kind != ir.KindIntersection {
		t.Fatalf("expected KindIntersection, got %v", weird.Type.Kind)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — schemas with `AllOf` and no `Type` currently fall to `buildObjectNode` via the `""` case, ignoring `AllOf` entirely, so `Dog` builds with zero fields and no `Extends`.

- [ ] **Step 3: Write the implementation**

In `internal/ir/build.go`, add an `AllOf` check to `buildNode`, before the `Enum` check:

```go
	if len(s.AllOf) > 0 {
		return buildAllOfNode(raw, s)
	}
	if len(s.Enum) > 0 {
```

Add the new function:

```go
func buildAllOfNode(raw *spec.RawDocument, s *spec.RawSchema) (*Node, error) {
	hasNonObject := false
	memberNodes := make([]*Node, 0, len(s.AllOf))
	var extends []string
	var fields []*Field
	seen := make(map[string]bool)

	addFields := func(fs []*Field) {
		for _, f := range fs {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}

	for i, member := range s.AllOf {
		var node *Node
		if member.Ref != "" {
			name, err := raw.ResolveRefName(member.Ref)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = &Node{Kind: KindRef, RefName: name}
			extends = append(extends, name)
		} else {
			built, err := buildNode(raw, member)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = built
			if node.Kind == KindObject {
				addFields(node.Object.Fields)
				extends = append(extends, node.Object.Extends...)
			} else {
				hasNonObject = true
			}
		}
		memberNodes = append(memberNodes, node)
	}

	own, err := buildObjectNode(raw, s)
	if err != nil {
		return nil, fmt.Errorf("allOf own properties: %w", err)
	}
	addFields(own.Object.Fields)

	if hasNonObject {
		if len(own.Object.Fields) > 0 {
			memberNodes = append(memberNodes, own)
		}
		return &Node{Kind: KindIntersection, Intersection: &IntersectionNode{Members: memberNodes}}, nil
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Extends: extends, Fields: fields}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go
git commit -m "feat: build allOf schemas into IR extends/intersection"
```

---

## Task 8: IR builder — oneOf/anyOf unions

**Files:**
- Modify: `internal/ir/build.go`
- Modify: `internal/ir/build_test.go`

**Interfaces:**
- Consumes: `ir.KindUnion`, `ir.UnionNode` (Task 4).
- Produces: `buildNode` now handles non-empty `OneOf`/`AnyOf`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ir/build_test.go`:

```go
func TestBuild_OneOfUnion(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {
				OneOf: []*spec.RawSchema{
					{Type: "string"},
					{Type: "number"},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindUnion {
		t.Fatalf("expected KindUnion, got %v", node.Kind)
	}
	if len(node.Union.Variants) != 2 {
		t.Errorf("expected 2 variants, got %d", len(node.Union.Variants))
	}
}

func TestBuild_AnyOfUnion(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				AnyOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Dog"},
					{Ref: "#/components/schemas/Cat"},
				},
			},
			"Dog": {Type: "object"},
			"Cat": {Type: "object"},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	pet := modelByName(t, doc, "Pet")
	if pet.Type.Kind != ir.KindUnion {
		t.Fatalf("expected KindUnion, got %v", pet.Type.Kind)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/...`
Expected: FAIL — schemas with `OneOf`/`AnyOf` and no `Type` currently fall to `buildObjectNode`, producing an empty object instead of a union.

- [ ] **Step 3: Write the implementation**

In `internal/ir/build.go`, add `OneOf`/`AnyOf` checks to `buildNode`, after the `AllOf` check:

```go
	if len(s.OneOf) > 0 {
		return buildUnionNode(raw, s.OneOf)
	}
	if len(s.AnyOf) > 0 {
		return buildUnionNode(raw, s.AnyOf)
	}
```

Add the new function:

```go
func buildUnionNode(raw *spec.RawDocument, variants []*spec.RawSchema) (*Node, error) {
	nodes := make([]*Node, 0, len(variants))
	for i, v := range variants {
		n, err := buildNode(raw, v)
		if err != nil {
			return nil, fmt.Errorf("variant[%d]: %w", i, err)
		}
		nodes = append(nodes, n)
	}
	return &Node{Kind: KindUnion, Union: &UnionNode{Variants: nodes}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ir/build.go internal/ir/build_test.go
git commit -m "feat: build oneOf/anyOf schemas into IR UnionNode"
```

---

## Task 9: TypeScript generator — objects, primitives, refs, identifiers

**Files:**
- Create: `internal/gen/ts/identifier.go`
- Create: `internal/gen/ts/generate.go`
- Test: `internal/gen/ts/identifier_test.go`
- Test: `internal/gen/ts/generate_test.go`

**Interfaces:**
- Consumes: `ir.Document`, `ir.Model`, `ir.Node`, `ir.Kind`, `ir.Primitive`, `ir.ObjectNode`, `ir.Field` (Task 4).
- Produces: `ts.SanitizeIdentifier(name string) string`, `ts.ModelOutput{Name string, Declaration string, Dependencies []string}`, `ts.Generate(doc *ir.Document) ([]ModelOutput, error)`.
  - This task implements object/interface rendering, primitive types, ref types, and required/optional/nullable fields. Enum/array/union/intersection/extends rendering land in Tasks 10-11, but the full `renderType` dispatch switch is written now (with those cases as no-op/unreachable until later tasks populate the corresponding IR kinds).

- [ ] **Step 1: Write the failing tests**

Create `internal/gen/ts/identifier_test.go`:

```go
package ts_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
)

func TestSanitizeIdentifier(t *testing.T) {
	cases := map[string]string{
		"Pet":        "Pet",
		"Pet-Status": "Pet_Status",
		"2Pets":      "_2Pets",
		"":           "_",
	}
	for input, want := range cases {
		got := ts.SanitizeIdentifier(input)
		if got != want {
			t.Errorf("SanitizeIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}
```

Create `internal/gen/ts/generate_test.go`:

```go
package ts_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

func TestGenerate_SimpleInterface(t *testing.T) {
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	want := "export interface Pet {\n  id?: number;\n  name: string;\n}\n"
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := outputs[0]
	want := "export interface Pet {\n  category?: Category;\n}\n"
	if out.Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.Declaration, want)
	}
	if len(out.Dependencies) != 1 || out.Dependencies[0] != "Category" {
		t.Errorf("expected Dependencies [Category], got %v", out.Dependencies)
	}
}

func TestGenerate_NullableField(t *testing.T) {
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export interface Account {\n  note: string | null;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_PrimitiveAlias(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "PetId", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type PetId = string;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gen/ts/...`
Expected: FAIL — package `ts` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/gen/ts/identifier.go`:

```go
package ts

import "regexp"

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_$]`)

// SanitizeIdentifier converts an arbitrary schema name into a valid
// TypeScript identifier: invalid characters become underscores, and a
// leading digit gets an underscore prefix.
func SanitizeIdentifier(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	return sanitized
}
```

Create `internal/gen/ts/generate.go`:

```go
package ts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level TypeScript declaration.
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
}

// Generate renders every model in doc as a TypeScript declaration, in the
// same order as doc.Models (name-sorted, so output is deterministic).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	outputs := make([]ModelOutput, 0, len(doc.Models))
	for _, m := range doc.Models {
		out, err := renderModel(m)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", m.Name, err)
		}
		out.Dependencies = removeName(out.Dependencies, out.Name)
		outputs = append(outputs, out)
	}
	return outputs, nil
}

func renderModel(m *ir.Model) (ModelOutput, error) {
	name := SanitizeIdentifier(m.Name)
	if m.Type.Kind == ir.KindObject {
		return renderInterface(name, m.Type.Object), nil
	}
	return renderTypeAlias(name, m.Type), nil
}

func renderInterface(name string, o *ir.ObjectNode) ModelOutput {
	var sb strings.Builder
	var deps []string
	sb.WriteString("export interface ")
	sb.WriteString(name)
	if len(o.Extends) > 0 {
		extendNames := make([]string, len(o.Extends))
		for i, e := range o.Extends {
			extendNames[i] = SanitizeIdentifier(e)
		}
		deps = append(deps, extendNames...)
		sb.WriteString(" extends ")
		sb.WriteString(strings.Join(extendNames, ", "))
	}
	sb.WriteString(" {\n")
	for _, f := range o.Fields {
		fieldExpr, fieldDeps := renderType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", SanitizeIdentifier(f.Name), optional, typeExpr))
	}
	sb.WriteString("}\n")
	return ModelOutput{Name: name, Declaration: sb.String(), Dependencies: dedupeSorted(deps)}
}

func renderTypeAlias(name string, node *ir.Node) ModelOutput {
	expr, deps := renderType(node)
	decl := fmt.Sprintf("export type %s = %s;\n", name, expr)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}
}

func renderType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := SanitizeIdentifier(node.RefName)
		return name, []string{name}
	case ir.KindEnum:
		return renderEnum(node.Enum)
	case ir.KindArray:
		return renderArray(node.Array)
	case ir.KindUnion:
		return renderVariantList(node.Union.Variants, " | ")
	case ir.KindIntersection:
		return renderVariantList(node.Intersection.Members, " & ")
	case ir.KindObject:
		return renderInlineObjectFields(node.Object.Fields)
	default:
		return "unknown", nil
	}
}

func renderPrimitive(p ir.Primitive) string {
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

func renderEnum(e *ir.EnumNode) (string, []string) {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		parts[i] = strconv.Quote(v)
	}
	return strings.Join(parts, " | "), nil
}

func renderArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := renderType(a.Items)
	return wrapForCombinator(itemExpr, a.Items) + "[]", deps
}

func renderVariantList(nodes []*ir.Node, sep string) (string, []string) {
	parts := make([]string, len(nodes))
	var deps []string
	for i, n := range nodes {
		expr, d := renderType(n)
		parts[i] = wrapForCombinator(expr, n)
		deps = append(deps, d...)
	}
	return strings.Join(parts, sep), deps
}

// renderInlineObjectFields renders a nested (non-top-level) object type as
// an inline TS object literal type. Note: an Extends list on a nested
// object (from a property whose own schema uses allOf) has no inline TS
// equivalent to `interface ... extends ...` and is not rendered here —
// only fields are. Top-level allOf-with-extends is handled by
// renderInterface, which is the common, supported pattern.
func renderInlineObjectFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := renderType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("%s%s: %s;", SanitizeIdentifier(f.Name), optional, typeExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

// wrapForCombinator parens-wraps a rendered type expression when it needs
// grouping to be embedded correctly inside an array (`(A | B)[]`) or
// another combinator (`(A | B) | null`).
func wrapForCombinator(expr string, n *ir.Node) string {
	if n.Kind == ir.KindUnion || n.Kind == ir.KindIntersection {
		return "(" + expr + ")"
	}
	return expr
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gen/ts/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gen/ts/identifier.go internal/gen/ts/generate.go internal/gen/ts/identifier_test.go internal/gen/ts/generate_test.go
git commit -m "feat: generate TS interfaces for objects, primitives, refs, nullable/optional fields"
```

---

## Task 10: TypeScript generator — enums and arrays

**Files:**
- Modify: `internal/gen/ts/generate_test.go`

**Interfaces:**
- Consumes: `renderEnum`, `renderArray`, `wrapForCombinator` (already written in Task 9; this task only adds coverage — `renderType`'s `KindEnum`/`KindArray` cases already dispatch to them).

- [ ] **Step 1: Write the failing tests**

Add to `internal/gen/ts/generate_test.go`:

```go
func TestGenerate_Enum(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"active", "inactive"}}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Status = \"active\" | \"inactive\";\n"
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export interface Pet {\n  tags?: Tag[];\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Tag" {
		t.Errorf("expected Dependencies [Tag], got %v", outputs[0].Dependencies)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/gen/ts/...`
Expected: These should already PASS, since Task 9 wrote the full `renderType` dispatch including `KindEnum` and `KindArray`. This step exists to confirm that — if either test fails, it reveals a bug in Task 9's `renderEnum`/`renderArray`/`wrapForCombinator` that must be fixed before continuing.

- [ ] **Step 3: Run full package test suite**

Run: `go test ./internal/gen/ts/...`
Expected: PASS (all tests, old and new)

- [ ] **Step 4: Commit**

```bash
git add internal/gen/ts/generate_test.go
git commit -m "test: add enum and array coverage for TS generator"
```

---

## Task 11: TypeScript generator — unions, intersections, allOf extends

**Files:**
- Modify: `internal/gen/ts/generate_test.go`

**Interfaces:**
- Consumes: `renderVariantList`, `wrapForCombinator`, `Extends` handling in `renderInterface` (already written in Task 9; this task adds coverage for `KindUnion`, `KindIntersection`, and `ObjectNode.Extends`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/gen/ts/generate_test.go`:

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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type StringOrNumber = string | number;\n"
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Weird = Base & string;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
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
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export interface Dog extends Animal {\n  breed: string;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Animal" {
		t.Errorf("expected Dependencies [Animal], got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_ArrayOfUnionWrapsInParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Values",
				Type: &ir.Node{
					Kind: ir.KindArray,
					Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{Variants: []*ir.Node{
						{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString},
						{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveNumber},
					}}}},
				},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Values = (string | number)[];\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}
```

- [ ] **Step 2: Run tests to check status**

Run: `go test ./internal/gen/ts/...`
Expected: These should already PASS, since Task 9 wrote the full rendering logic including `Extends`, `KindUnion`, `KindIntersection`, and combinator-wrapping. If any fail, fix the corresponding function in `internal/gen/ts/generate.go` (most likely `wrapForCombinator`, `renderVariantList`, or the `Extends` branch of `renderInterface`) before continuing — do not skip a failure here.

- [ ] **Step 3: Run full package test suite**

Run: `go test ./internal/gen/ts/...`
Expected: PASS (all tests)

- [ ] **Step 4: Commit**

```bash
git add internal/gen/ts/generate_test.go
git commit -m "test: add union, intersection, and allOf-extends coverage for TS generator"
```

---

## Task 12: pkg/engine — Parse + GenerateTS orchestration (monolithic output)

**Files:**
- Create: `pkg/engine/engine.go`
- Create: `pkg/engine/output.go`
- Test: `pkg/engine/engine_test.go`

**Interfaces:**
- Consumes: `spec.Parse` (Task 3), `ir.Build`, `ir.Document` (Task 4), `ts.Generate`, `ts.ModelOutput` (Task 9).
- Produces: `engine.Parse(data []byte) (*ir.Document, error)`, `engine.TSOptions{Modular bool}`, `engine.Output{Files map[string]string}`, `engine.GenerateTS(doc *ir.Document, opts TSOptions) (Output, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/engine/engine_test.go`:

```go
package engine_test

import (
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

const simpleSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Pet": {
        "type": "object",
        "properties": {
          "name": {"type": "string"}
        },
        "required": ["name"]
      }
    }
  }
}`

func TestParseAndGenerateTS_Monolithic(t *testing.T) {
	doc, err := engine.Parse([]byte(simpleSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	content, ok := out.Files["index.ts"]
	if !ok {
		t.Fatal("expected index.ts in output")
	}
	if !strings.Contains(content, "export interface Pet {") {
		t.Errorf("expected Pet interface, got:\n%s", content)
	}
	if len(out.Files) != 1 {
		t.Errorf("expected exactly 1 file for monolithic output, got %d", len(out.Files))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/engine/...`
Expected: FAIL — package `engine` does not exist.

- [ ] **Step 3: Write the implementation**

Create `pkg/engine/engine.go`:

```go
// Package engine is the public API of the OpenAPI2Code core: parsing an
// OpenAPI/Swagger document into IR, and generating target-language code
// from that IR. It does no file I/O or network access, so it is safe to
// compile unchanged to WASM.
package engine

import (
	"github.com/tarikomercehajic/openapi2code/internal/ir"
	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

// Parse parses raw OpenAPI/Swagger document bytes (JSON or YAML, v2 or
// v3) into the IR.
func Parse(data []byte) (*ir.Document, error) {
	raw, err := spec.Parse(data)
	if err != nil {
		return nil, err
	}
	return ir.Build(raw)
}

// TSOptions controls TypeScript output shape.
type TSOptions struct {
	// Modular selects one-file-per-model output (with a barrel index.ts)
	// instead of a single monolithic file.
	Modular bool
}

// Output is a generated set of files, keyed by relative file path.
type Output struct {
	Files map[string]string
}
```

Create `pkg/engine/output.go`:

```go
package engine

import (
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// GenerateTS renders doc as TypeScript, laid out per opts.
//
// Note: opts.Modular is not yet consulted — Task 13 adds the modular
// branch alongside modularOutput. Until then this always produces
// monolithic output, which is all Task 12's own test exercises.
func GenerateTS(doc *ir.Document, opts TSOptions) (Output, error) {
	models, err := ts.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	return monolithicOutput(models), nil
}

func monolithicOutput(models []ts.ModelOutput) Output {
	var sb strings.Builder
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
git commit -m "feat: add pkg/engine with Parse and monolithic GenerateTS"
```

---

## Task 13: pkg/engine — modular output mode + barrel index.ts

**Files:**
- Modify: `pkg/engine/output.go`
- Modify: `pkg/engine/engine_test.go`

**Interfaces:**
- Consumes: `ts.ModelOutput{Name, Declaration, Dependencies}` (Task 9), `Output`, `GenerateTS` (Task 12).
- Produces: `modularOutput`, wired into `GenerateTS` (which this task also modifies to branch on `opts.Modular`).

- [ ] **Step 1: Write the failing test**

Add to `pkg/engine/engine_test.go`:

```go
const twoModelSpec = `{
  "openapi": "3.0.3",
  "components": {
    "schemas": {
      "Category": {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"]
      },
      "Pet": {
        "type": "object",
        "properties": {
          "category": {"$ref": "#/components/schemas/Category"}
        }
      }
    }
  }
}`

func TestGenerateTS_Modular(t *testing.T) {
	doc, err := engine.Parse([]byte(twoModelSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: true})
	if err != nil {
		t.Fatalf("GenerateTS: %v", err)
	}
	if _, ok := out.Files["Category.ts"]; !ok {
		t.Error("expected Category.ts")
	}
	petFile, ok := out.Files["Pet.ts"]
	if !ok {
		t.Fatal("expected Pet.ts")
	}
	if !strings.Contains(petFile, `import type { Category } from "./Category";`) {
		t.Errorf("expected import statement in Pet.ts, got:\n%s", petFile)
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
Expected: FAIL — `GenerateTS` currently always calls `monolithicOutput` regardless of `opts.Modular`, so `Category.ts`/`Pet.ts` are absent from `out.Files`.

- [ ] **Step 3: Write the implementation**

In `pkg/engine/output.go`, add the import `"fmt"`, update `GenerateTS` to branch on `opts.Modular`, and add `modularOutput`:

```go
import (
	"fmt"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)
```

```go
// GenerateTS renders doc as TypeScript, laid out per opts.
func GenerateTS(doc *ir.Document, opts TSOptions) (Output, error) {
	models, err := ts.Generate(doc)
	if err != nil {
		return Output{}, err
	}
	if opts.Modular {
		return modularOutput(models), nil
	}
	return monolithicOutput(models), nil
}
```

```go
func modularOutput(models []ts.ModelOutput) Output {
	files := make(map[string]string, len(models)+1)
	var barrel strings.Builder
	for _, m := range models {
		fileName := m.Name + ".ts"
		var content strings.Builder
		for _, dep := range m.Dependencies {
			content.WriteString(fmt.Sprintf("import type { %s } from \"./%s\";\n", dep, dep))
		}
		if len(m.Dependencies) > 0 {
			content.WriteString("\n")
		}
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
git commit -m "feat: add modular TS output mode with per-model files and barrel index.ts"
```

---

## Task 14: Golden-file end-to-end test suite

**Files:**
- Create: `pkg/engine/testdata/petstore.yaml`
- Create: `pkg/engine/testdata/composition.yaml`
- Create: `pkg/engine/testdata/circular.yaml`
- Create: `pkg/engine/testdata/nullable_enum.yaml`
- Create: `pkg/engine/golden_test.go`
- Create (generated by Step 4, committed in Step 6): `pkg/engine/testdata/golden/petstore.ts`, `pkg/engine/testdata/golden/composition.ts`, `pkg/engine/testdata/golden/circular.ts`, `pkg/engine/testdata/golden/nullable_enum.ts`

**Interfaces:**
- Consumes: `engine.Parse`, `engine.GenerateTS`, `engine.TSOptions` (Tasks 12-13).

- [ ] **Step 1: Write the fixture specs**

Create `pkg/engine/testdata/petstore.yaml`:

```yaml
openapi: "3.0.3"
info:
  title: Petstore-style fixture
  version: "1.0.0"
components:
  schemas:
    Category:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Tag:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - name
    Pet:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
        category:
          $ref: "#/components/schemas/Category"
        tags:
          type: array
          items:
            $ref: "#/components/schemas/Tag"
        photoUrls:
          type: array
          items:
            type: string
        status:
          type: string
          enum:
            - available
            - pending
            - sold
      required:
        - name
        - photoUrls
    Error:
      type: object
      properties:
        code:
          type: integer
        message:
          type: string
      required:
        - code
        - message
```

Create `pkg/engine/testdata/composition.yaml`:

```yaml
openapi: "3.0.3"
info:
  title: Composition fixture
  version: "1.0.0"
components:
  schemas:
    Animal:
      type: object
      properties:
        name:
          type: string
      required:
        - name
    Dog:
      allOf:
        - $ref: "#/components/schemas/Animal"
        - type: object
          properties:
            breed:
              type: string
          required:
            - breed
    StringOrNumber:
      oneOf:
        - type: string
        - type: number
    Pet:
      anyOf:
        - $ref: "#/components/schemas/Dog"
        - $ref: "#/components/schemas/Animal"
```

Create `pkg/engine/testdata/circular.yaml`:

```yaml
openapi: "3.0.3"
info:
  title: Circular fixture
  version: "1.0.0"
components:
  schemas:
    TreeNode:
      type: object
      properties:
        value:
          type: string
        children:
          type: array
          items:
            $ref: "#/components/schemas/TreeNode"
      required:
        - value
    A:
      type: object
      properties:
        b:
          $ref: "#/components/schemas/B"
    B:
      type: object
      properties:
        a:
          $ref: "#/components/schemas/A"
```

Create `pkg/engine/testdata/nullable_enum.yaml`:

```yaml
openapi: "3.0.3"
info:
  title: Nullable and enum fixture
  version: "1.0.0"
components:
  schemas:
    Status:
      type: string
      enum:
        - active
        - inactive
        - pending
    Account:
      type: object
      properties:
        id:
          type: string
        status:
          $ref: "#/components/schemas/Status"
        nickname:
          type: string
          nullable: true
        note:
          type: string
          nullable: true
      required:
        - id
        - status
        - note
```

- [ ] **Step 2: Write the golden-file test harness**

Create `pkg/engine/golden_test.go`:

```go
package engine_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tarikomercehajic/openapi2code/pkg/engine"
)

var update = flag.Bool("update", false, "update golden files")

func TestGoldenFixtures(t *testing.T) {
	fixtures := []string{"petstore", "composition", "circular", "nullable_enum"}
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
			out, err := engine.GenerateTS(doc, engine.TSOptions{Modular: false})
			if err != nil {
				t.Fatalf("GenerateTS: %v", err)
			}
			got := out.Files["index.ts"]
			goldenPath := filepath.Join("testdata", "golden", name+".ts")
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

- [ ] **Step 3: Confirm the test fails without golden files**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures`
Expected: FAIL — golden files don't exist yet (`read golden (run with -update to create it): ... no such file or directory`).

- [ ] **Step 4: Generate the golden files**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures -update`
Expected: PASS (this run only writes files, it doesn't compare).

- [ ] **Step 5: Manually verify the generated golden output**

Open each generated file under `pkg/engine/testdata/golden/` and check it against the expected shape below. If anything differs in a way that's a genuine generator bug (not just incidental whitespace), fix the relevant function in `internal/ir/build.go` or `internal/gen/ts/generate.go`, then re-run Step 4 to regenerate.

`petstore.ts` — 4 declarations in this order (models are name-sorted), fields sorted alphabetically within each interface:

```
export interface Category {
  id?: number;
  name: string;
}

export interface Error {
  code: number;
  message: string;
}

export interface Pet {
  category?: Category;
  id?: number;
  name: string;
  photoUrls: string[];
  status?: "available" | "pending" | "sold";
  tags?: Tag[];
}

export interface Tag {
  id?: number;
  name: string;
}
```

`composition.ts` — 4 declarations, name-sorted (Animal, Dog, Pet, StringOrNumber):

```
export interface Animal {
  name: string;
}

export interface Dog extends Animal {
  breed: string;
}

export type Pet = Dog | Animal;

export type StringOrNumber = string | number;
```

`circular.ts` — 3 declarations, name-sorted (A, B, TreeNode); no special syntax needed for cycles since TS interfaces reference each other by name natively:

```
export interface A {
  b?: B;
}

export interface B {
  a?: A;
}

export interface TreeNode {
  children?: TreeNode[];
  value: string;
}
```

`nullable_enum.ts` — 2 declarations, name-sorted (Account, Status); note `nickname` is optional+nullable, `note` is required+nullable:

```
export interface Account {
  id: string;
  nickname?: string | null;
  note: string | null;
  status: Status;
}

export type Status = "active" | "inactive" | "pending";
```

- [ ] **Step 6: Run the golden test suite for real**

Run: `go test ./pkg/engine/... -run TestGoldenFixtures`
Expected: PASS

- [ ] **Step 7: Run the full test suite**

Run: `go test ./...`
Expected: PASS (every package)

- [ ] **Step 8: Commit**

```bash
git add pkg/engine/testdata pkg/engine/golden_test.go
git commit -m "test: add golden-file end-to-end fixtures for petstore, composition, circular refs, and nullable/enum"
```

---

## Final verification

- [ ] Run `go build ./...` — expect success with no errors.
- [ ] Run `go vet ./...` — expect no warnings.
- [ ] Run `go test ./...` — expect all packages PASS.
