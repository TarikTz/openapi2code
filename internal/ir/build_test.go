package ir_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

// TestBuild_IntegerEnumPreservesPrimitive guards against enum values
// being treated as string-typed regardless of the schema's declared
// "type" — every generator renders a string-typed enum's values as
// quoted string literals, which is wrong for an integer/number/boolean
// enum (e.g. TS's `"1" | "2"` instead of `1 | 2`, or Zod's
// z.enum(["1","2"]) rejecting the real number 1 at runtime).
func TestBuild_IntegerEnumPreservesPrimitive(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Priority": {Type: "integer", Enum: []interface{}{float64(1), float64(2), float64(3)}},
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
	if node.Enum.Primitive != ir.PrimitiveInteger {
		t.Errorf("got Primitive %v, want PrimitiveInteger", node.Enum.Primitive)
	}
	if len(node.Enum.Values) != 3 || node.Enum.Values[0] != "1" || node.Enum.Values[2] != "3" {
		t.Errorf("got %v, want [1 2 3]", node.Enum.Values)
	}
}

// TestBuild_EnumWithNullValueIsDropped guards the OpenAPI 3.0
// nullable-enum workaround (a literal null entry alongside
// nullable: true, since 3.0 has no `type: [T, null]`). Before the fix,
// fmt.Sprint on the nil interface{} value produced the literal string
// "<nil>", leaking Go's internal nil-formatting into every generator's
// enum output as a bogus member.
func TestBuild_EnumWithNullValueIsDropped(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Status": {Type: "string", Nullable: true, Enum: []interface{}{"active", "inactive", nil}},
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
	for _, v := range node.Enum.Values {
		if strings.Contains(v, "nil") {
			t.Fatalf("null enum entry leaked as %q, want it dropped: %v", v, node.Enum.Values)
		}
	}
	if len(node.Enum.Values) != 2 || node.Enum.Values[0] != "active" || node.Enum.Values[1] != "inactive" {
		t.Errorf("got %v, want [active inactive]", node.Enum.Values)
	}
}

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

// TestBuild_AdditionalPropertiesSchemaBuildsMap covers the primary
// additionalProperties case: a schema with no properties of its own and
// a schema-object additionalProperties value builds as a KindMap rather
// than an empty KindObject that silently discards every key a real
// payload would carry.
func TestBuild_AdditionalPropertiesSchemaBuildsMap(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Metadata": {
				Type:                 "object",
				AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Type: "string"}, Allowed: true},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindMap {
		t.Fatalf("expected KindMap, got %v", node.Kind)
	}
	if node.Map.Values.Kind != ir.KindPrimitive || node.Map.Values.Primitive != ir.PrimitiveString {
		t.Errorf("expected string value type, got %+v", node.Map.Values)
	}
}

// TestBuild_AdditionalPropertiesRefValueBuildsMapOfRef covers a map whose
// values are a $ref (the common "map of named model" shape), and pins
// that the ref itself is left unresolved (KindRef), matching how object
// fields already treat a $ref value — the generator layer resolves it.
func TestBuild_AdditionalPropertiesRefValueBuildsMapOfRef(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Tag": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}},
			"TagsByID": {
				Type:                 "object",
				AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Ref: "#/components/schemas/Tag"}, Allowed: true},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	tagsByID := modelByName(t, doc, "TagsByID")
	if tagsByID.Type.Kind != ir.KindMap {
		t.Fatalf("expected KindMap, got %v", tagsByID.Type.Kind)
	}
	if tagsByID.Type.Map.Values.Kind != ir.KindRef || tagsByID.Type.Map.Values.RefName != "Tag" {
		t.Errorf("expected a ref to Tag as the value type, got %+v", tagsByID.Type.Map.Values)
	}
}

// TestBuild_AdditionalPropertiesBareTrueStaysPlainObject pins the
// deliberate scoping decision: additionalProperties: true (no schema)
// has no well-defined value type this generator can render consistently
// across all five targets, so it's left as an ordinary (here, empty)
// object rather than becoming a map of "unknown".
func TestBuild_AdditionalPropertiesBareTrueStaysPlainObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Freeform": {Type: "object", AdditionalProperties: &spec.AdditionalProperties{Allowed: true}},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindObject {
		t.Fatalf("expected KindObject (deliberately not a map), got %v", node.Kind)
	}
}

// TestBuild_AdditionalPropertiesFalseStaysPlainObject pins the same
// scoping decision for the other boolean form: additionalProperties:
// false explicitly means "no extra properties", so it must never be
// treated as a map.
func TestBuild_AdditionalPropertiesFalseStaysPlainObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Closed": {Type: "object", AdditionalProperties: &spec.AdditionalProperties{Allowed: false}},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindObject {
		t.Fatalf("expected KindObject, got %v", node.Kind)
	}
}

// TestBuild_PropertiesWithAdditionalPropertiesStaysPlainObject pins the
// other deliberate scoping decision: a schema with BOTH fixed properties
// AND additionalProperties (a struct plus a catch-all bucket) is left as
// an ordinary object using only the fixed properties — see
// docs/superpowers/specs/2026-09-10-future-considerations.md.
func TestBuild_PropertiesWithAdditionalPropertiesStaysPlainObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type:                 "object",
				Properties:           map[string]*spec.RawSchema{"name": {Type: "string"}},
				AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Type: "string"}, Allowed: true},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	node := doc.Models[0].Type
	if node.Kind != ir.KindObject {
		t.Fatalf("expected KindObject, got %v", node.Kind)
	}
	if len(node.Object.Fields) != 1 || node.Object.Fields[0].Name != "name" {
		t.Errorf("expected only the fixed 'name' field, got %v", node.Object.Fields)
	}
}

// TestBuild_AllOfWithMapMemberFallsBackToIntersection guards that a map-
// shaped allOf member is treated as non-object, the same as any other
// non-object shape — schemaIsObjectShaped and buildAllOfNode's inline-
// member check must agree a KindMap is not KindObject.
func TestBuild_AllOfWithMapMemberFallsBackToIntersection(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Base": {Type: "object", Properties: map[string]*spec.RawSchema{"id": {Type: "string"}}},
			"Weird": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Base"},
					{Type: "object", AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Type: "string"}, Allowed: true}},
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

// TestBuild_MapOfSelfCreatesCycle mirrors
// TestBuild_ArrayOfSelfCreatesCycle for KindMap: markCycles' cycleWalk
// must recurse into a map's value type the same way it already does for
// an array's item type, or a cyclic reference reached only through a
// map field would go undetected.
func TestBuild_MapOfSelfCreatesCycle(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"TreeNode": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"children": {
						Type:                 "object",
						AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Ref: "#/components/schemas/TreeNode"}, Allowed: true},
					},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !doc.Models[0].Cyclic {
		t.Error("expected TreeNode to be marked Cyclic via map value ref")
	}
}

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

// TestBuild_AllOfWithNonObjectRefMemberFallsBackToIntersection guards a
// ref (not inline) allOf member that resolves to a non-object schema
// (here an enum): buildAllOfNode must fall back to KindIntersection the
// same way it does for an inline non-object member, instead of blindly
// adding the ref to Extends. Before the fix, this incorrectly produced
// KindObject{Extends: ["StatusEnum"]} — every generator that renders
// Extends as inheritance would then reference a non-object base.
func TestBuild_AllOfWithNonObjectRefMemberFallsBackToIntersection(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StatusEnum": {Type: "string", Enum: []interface{}{"active", "inactive"}},
			"Dog": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/StatusEnum"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"breed": {Type: "string"}}},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dog := modelByName(t, doc, "Dog")
	if dog.Type.Kind != ir.KindIntersection {
		t.Fatalf("expected KindIntersection, got %v (Extends=%v)", dog.Type.Kind, dog.Type.Object)
	}
}

// TestBuild_MutualAllOfExtendsRefStillObjectShaped guards the opposite
// direction of the same fix: a $ref member whose target is only
// reachable by following a cyclic allOf-extends chain back to the model
// currently being built must still be treated as object-shaped (not
// forced into KindIntersection merely because the classification walk
// hit a cycle) — mutually-extending models are an existing supported
// pattern (see TestBuild_AllOfExtendsCycleMarked).
func TestBuild_MutualAllOfExtendsRefStillObjectShaped(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"A": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/B"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"extra": {Type: "string"}}},
				},
			},
			"B": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/A"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"other": {Type: "string"}}},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	a := modelByName(t, doc, "A")
	if a.Type.Kind != ir.KindObject {
		t.Fatalf("expected KindObject for A despite the extends cycle, got %v", a.Type.Kind)
	}
	if len(a.Type.Object.Extends) != 1 || a.Type.Object.Extends[0] != "B" {
		t.Errorf("expected A to extend B, got %v", a.Type.Object.Extends)
	}
}

func TestBuild_SingleRefAllOfCollapsesToRef(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StatusEnum": {Type: "string", Enum: []interface{}{"active", "inactive"}},
			"Task": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"status": {
						AllOf: []*spec.RawSchema{
							{Ref: "#/components/schemas/StatusEnum"},
						},
					},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	task := modelByName(t, doc, "Task")
	if task.Type.Kind != ir.KindObject {
		t.Fatalf("expected KindObject for Task, got %v", task.Type.Kind)
	}
	if len(task.Type.Object.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(task.Type.Object.Fields))
	}
	field := task.Type.Object.Fields[0]
	if field.Name != "status" {
		t.Fatalf("expected field name 'status', got %q", field.Name)
	}
	if field.Type.Kind != ir.KindRef {
		t.Fatalf("expected KindRef for status field, got %v", field.Type.Kind)
	}
	if field.Type.RefName != "StatusEnum" {
		t.Fatalf("expected RefName 'StatusEnum', got %q", field.Type.RefName)
	}
}

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

func TestBuild_AcyclicDiamondNotMarkedCyclic(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Root": {Type: "object", Properties: map[string]*spec.RawSchema{
				"left":  {Ref: "#/components/schemas/Left"},
				"right": {Ref: "#/components/schemas/Right"},
			}},
			"Left":   {Type: "object", Properties: map[string]*spec.RawSchema{"shared": {Ref: "#/components/schemas/Shared"}}},
			"Right":  {Type: "object", Properties: map[string]*spec.RawSchema{"shared": {Ref: "#/components/schemas/Shared"}}},
			"Shared": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}},
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

// TestBuild_DeepDiamondChainIsNotExponential guards against reintroducing
// the exponential cycle-detection walk: each model refs the next two, so
// the number of distinct paths through the (acyclic) graph grows like
// Fibonacci, and a walk without negative memoization takes minutes here.
func TestBuild_DeepDiamondChainIsNotExponential(t *testing.T) {
	const n = 40
	schemas := make(map[string]*spec.RawSchema, n)
	for i := 0; i < n; i++ {
		props := map[string]*spec.RawSchema{}
		if i+1 < n {
			props["next"] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%02d", i+1)}
		}
		if i+2 < n {
			props["skip"] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%02d", i+2)}
		}
		schemas[fmt.Sprintf("M%02d", i)] = &spec.RawSchema{Type: "object", Properties: props}
	}

	start := time.Now()
	doc, err := ir.Build(&spec.RawDocument{Schemas: schemas})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("Build took %v for an acyclic %d-model chain; cycle detection is not linear", elapsed, n)
	}
	for _, m := range doc.Models {
		if m.Cyclic {
			t.Errorf("expected %s to not be marked Cyclic", m.Name)
		}
	}
}

// TestBuild_DenselyCyclicDiamondIsNotExponential is
// TestBuild_DeepDiamondChainIsNotExponential's cyclic counterpart, and
// the one that actually reproduces the reported real-world bug: the
// acyclic diamond chain above was already handled by the old
// per-model reachability search's noReach memoization (that memoization
// is exactly what collapsed the acyclic diamond's exponential path
// count). What it could never cache was a result reached by crossing a
// genuine cycle ELSEWHERE in the graph — every model here also reaches
// a small CycleA/CycleB mutual pair, unrelated to whether that model
// itself is cyclic, and the old algorithm re-explored each one's entire
// subtree on every single diamond path that reached it. Measured before
// the fix: 72ms at n=25, 3.4s at n=33 (exponential growth matching the
// Fibonacci path count almost exactly) — extrapolating that curve lands
// squarely on the 400+ seconds measured against Stripe's real,
// similarly densely-cross-referenced 1454-schema spec.
func TestBuild_DenselyCyclicDiamondIsNotExponential(t *testing.T) {
	const n = 40
	schemas := make(map[string]*spec.RawSchema, n+2)
	for i := 0; i < n; i++ {
		props := map[string]*spec.RawSchema{
			"cyclic": {Ref: "#/components/schemas/CycleA"},
		}
		if i+1 < n {
			props["next"] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%02d", i+1)}
		}
		if i+2 < n {
			props["skip"] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%02d", i+2)}
		}
		schemas[fmt.Sprintf("M%02d", i)] = &spec.RawSchema{Type: "object", Properties: props}
	}
	schemas["CycleA"] = &spec.RawSchema{Type: "object", Properties: map[string]*spec.RawSchema{
		"b": {Ref: "#/components/schemas/CycleB"},
	}}
	schemas["CycleB"] = &spec.RawSchema{Type: "object", Properties: map[string]*spec.RawSchema{
		"a": {Ref: "#/components/schemas/CycleA"},
	}}

	start := time.Now()
	doc, err := ir.Build(&spec.RawDocument{Schemas: schemas})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("Build took %v for a %d-model graph with one small unrelated cycle; cycle detection is not linear", elapsed, n+2)
	}

	for _, m := range doc.Models {
		switch m.Name {
		case "CycleA", "CycleB":
			if !m.Cyclic {
				t.Errorf("expected %s to be marked Cyclic", m.Name)
			}
		default:
			if m.Cyclic {
				t.Errorf("expected %s to NOT be marked Cyclic (it reaches the unrelated CycleA/CycleB pair, but never a cycle back to itself)", m.Name)
			}
		}
	}
}

// TestBuild_StripeScaleDenselyInterconnectedGraphIsFast approximates the
// real spec (Stripe's, 1454 schemas) that measured 400+ seconds under
// the old per-model reachability search: each model refs several
// nearby ones both forward and backward, producing a graph that's
// genuinely, densely cyclic throughout rather than merely diamond-
// shaped. The fixed algorithm handles it in low single-digit
// milliseconds.
func TestBuild_StripeScaleDenselyInterconnectedGraphIsFast(t *testing.T) {
	const n = 1500
	schemas := make(map[string]*spec.RawSchema, n)
	for i := 0; i < n; i++ {
		props := map[string]*spec.RawSchema{}
		for _, off := range []int{1, 2, 3, -1, -2} {
			j := i + off
			if j >= 0 && j < n && j != i {
				props[fmt.Sprintf("r%d", off)] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%04d", j)}
			}
		}
		schemas[fmt.Sprintf("M%04d", i)] = &spec.RawSchema{Type: "object", Properties: props}
	}
	start := time.Now()
	_, err := ir.Build(&spec.RawDocument{Schemas: schemas})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("Build took %v for a %d-model densely cross-referenced graph; cycle detection is not linear", elapsed, n)
	}
}

// TestBuild_VeryLongRefChainDoesNotStackOverflow guards the disclosed
// WASM call-stack-overflow limitation (see
// docs/superpowers/specs/2026-09-10-future-considerations.md): a long
// chain of $ref-linked models used to recurse once per link via
// markCycles/cycleWalk. The strongly-connected-components rewrite that
// fixed the exponential-blowup bug also made cycle detection fully
// iterative (an explicit frame stack, no recursion at all), which this
// pins down natively; cmd/wasm's own TestGenerateJS_LongRefChainDoesNotOverflowWASMStack
// exercises the same shape against the real compiled WASM module, where
// the JS engine's much smaller call stack is what was actually
// overflowing.
func TestBuild_VeryLongRefChainDoesNotStackOverflow(t *testing.T) {
	const n = 10000
	schemas := make(map[string]*spec.RawSchema, n)
	for i := 0; i < n; i++ {
		props := map[string]*spec.RawSchema{}
		if i+1 < n {
			props["next"] = &spec.RawSchema{Ref: fmt.Sprintf("#/components/schemas/M%05d", i+1)}
		}
		schemas[fmt.Sprintf("M%05d", i)] = &spec.RawSchema{Type: "object", Properties: props}
	}
	if _, err := ir.Build(&spec.RawDocument{Schemas: schemas}); err != nil {
		t.Fatalf("Build: %v", err)
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

func nestedArraySchema(depth int) *spec.RawSchema {
	s := &spec.RawSchema{Type: "string"}
	for i := 0; i < depth; i++ {
		s = &spec.RawSchema{Type: "array", Items: s}
	}
	return s
}

func TestBuild_ExceedsMaxNestingDepthErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Deep": nestedArraySchema(501),
		},
	}
	_, err := ir.Build(raw)
	if err == nil {
		t.Fatal("expected an error for schema nesting beyond the max depth, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum depth") {
		t.Fatalf("expected a nesting-depth error, got: %v", err)
	}
}

func TestBuild_AtMaxNestingDepthSucceeds(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Deep": nestedArraySchema(500),
		},
	}
	_, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: unexpected error at max depth: %v", err)
	}
}

// TestBuild_NilSchemaErrors covers a spec with a named model whose body is
// null in the source document — e.g. typing "schemas:\n    Pet:" with
// nothing under it yet, which YAML/JSON decode as a nil *spec.RawSchema.
// Build must return a clean error instead of nil-pointer-dereference
// panicking, since this is a routine state while a user is typing a spec
// live in the web playground.
func TestBuild_NilSchemaErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": nil,
		},
	}
	_, err := ir.Build(raw)
	if err == nil {
		t.Fatal("expected an error for a nil schema, got nil")
	}
}

// TestBuild_NilPropertySchemaErrors covers the same incomplete-YAML shape
// one level down: a property whose value is null (e.g. "properties: { name:
// }"). buildObjectNode passes s.Properties[name] straight into buildNode
// without a nil check of its own, so this path is only safe because of the
// nil check at the top of buildNode.
func TestBuild_NilPropertySchemaErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"name": nil,
				},
			},
		},
	}
	_, err := ir.Build(raw)
	if err == nil {
		t.Fatal("expected an error for a nil property schema, got nil")
	}
}

// TestBuild_NilAllOfMemberSchemaErrors covers a nil entry inside an allOf
// list (e.g. "allOf:\n  - $ref: '#/...'\n  -\n"). Unlike the OneOf/AnyOf
// union path, buildAllOfNode dereferences each member's Ref field before
// ever calling buildNode, so a nil member needs its own guard rather than
// relying solely on the top-of-buildNode nil check.
func TestBuild_NilAllOfMemberSchemaErrors(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Base": {Type: "object", Properties: map[string]*spec.RawSchema{"id": {Type: "string"}}},
			"Weird": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Base"},
					nil,
				},
			},
		},
	}
	_, err := ir.Build(raw)
	if err == nil {
		t.Fatal("expected an error for a nil allOf member schema, got nil")
	}
}

func TestBuild_TypeUnrepresentableFieldBecomesPrimitiveUnknown(t *testing.T) {
	// Mirrors what spec.RawSchema's UnmarshalJSON sets for an OpenAPI
	// 3.1-style `type: ["string", "integer"]` (or `["null"]`) field: no
	// single generated type applies, so buildNode should skip it the
	// same way it already skips any other unrecognized primitive.
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Weird": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"multi": {TypeUnrepresentable: true},
				},
			},
		},
	}
	doc, err := ir.Build(raw)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	obj := doc.Models[0].Type.Object
	if len(obj.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(obj.Fields))
	}
	field := obj.Fields[0]
	if field.Type.Kind != ir.KindPrimitive || field.Type.Primitive != ir.PrimitiveUnknown {
		t.Errorf("got kind %v primitive %v, want KindPrimitive/PrimitiveUnknown", field.Type.Kind, field.Type.Primitive)
	}
}
