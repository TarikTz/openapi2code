package ts_test

import (
	"strings"
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

func TestGenerate_FieldNameNeedingQuotesIsQuotedNotMangled(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Headers",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "content-type", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
							{Name: "content_type", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
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
	want := "export interface Headers {\n  \"content-type\"?: string;\n  content_type?: string;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_InlineObjectFieldNameQuoted(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Envelope",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{
								Name: "headers",
								Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
									Fields: []*ir.Field{
										{Name: "content-type", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
									},
								}},
								Optional: true,
							},
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
	want := "export interface Envelope {\n  headers?: { \"content-type\"?: string; };\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_ReservedWordModelName(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "class", Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if outputs[0].Name != "class_" {
		t.Errorf("got name %q, want class_", outputs[0].Name)
	}
	want := "export interface class_ {\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_CollidingSanitizedNamesAreUniquified(t *testing.T) {
	// Both names sanitize to "Pet_Status"; neither model may be lost, and
	// the two declarations must not share an identifier.
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "Pet-Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"a"}}}},
			{Name: "Pet.Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"b"}}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}
	if outputs[0].Name != "Pet_Status" || outputs[1].Name != "Pet_Status_2" {
		t.Fatalf("expected [Pet_Status Pet_Status_2], got [%s %s]", outputs[0].Name, outputs[1].Name)
	}
	if outputs[0].Declaration != "export type Pet_Status = \"a\";\n" {
		t.Errorf("got %q", outputs[0].Declaration)
	}
	if outputs[1].Declaration != "export type Pet_Status_2 = \"b\";\n" {
		t.Errorf("got %q", outputs[1].Declaration)
	}
}

func TestGenerate_RefsToCollidingNamesResolveToUniqueNames(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Pet",
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "dashed", Type: &ir.Node{Kind: ir.KindRef, RefName: "Pet-Status"}, Optional: true},
					{Name: "dotted", Type: &ir.Node{Kind: ir.KindRef, RefName: "Pet.Status"}, Optional: true},
				}}},
			},
			{Name: "Pet-Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"a"}}}},
			{Name: "Pet.Status", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"b"}}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export interface Pet {\n  dashed?: Pet_Status;\n  dotted?: Pet_Status_2;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 2 || outputs[0].Dependencies[0] != "Pet_Status" || outputs[0].Dependencies[1] != "Pet_Status_2" {
		t.Errorf("expected Dependencies [Pet_Status Pet_Status_2], got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_ModelNamedIndexIsRenamed(t *testing.T) {
	// "index" is reserved for the modular barrel file, so a model of that
	// name must not claim index.ts.
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "index", Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if outputs[0].Name == "index" {
		t.Errorf("expected model named 'index' to be renamed, got %q", outputs[0].Name)
	}
	if !strings.Contains(outputs[0].Declaration, "export interface "+outputs[0].Name+" {") {
		t.Errorf("declaration does not use the uniquified name: %q", outputs[0].Declaration)
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

// TestGenerate_IntegerEnumRendersUnquotedLiterals guards against every
// enum being rendered as a string-literal union regardless of its
// declared type: an integer enum's values must appear as bare numeric
// literals (1 | 2), not quoted strings ("1" | "2"), which would reject
// every real (numeric) value of the field.
func TestGenerate_IntegerEnumRendersUnquotedLiterals(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{Name: "Priority", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"1", "2", "3"}, Primitive: ir.PrimitiveInteger}}},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Priority = 1 | 2 | 3;\n"
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

// TestGenerate_MapField covers a field built from additionalProperties
// (ir.KindMap), rendered as TypeScript's Record utility type.
func TestGenerate_MapField(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Pet",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "metadata", Type: &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}}}, Optional: true},
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
	want := "export interface Pet {\n  metadata?: Record<string, string>;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

// TestGenerate_TopLevelMapOfRef covers a named top-level map schema whose
// values are a $ref, checking both the rendered type alias and that the
// ref is tracked as a dependency.
func TestGenerate_TopLevelMapOfRef(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "TagsByID",
				Type: &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: &ir.Node{Kind: ir.KindRef, RefName: "Tag"}}},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type TagsByID = Record<string, Tag>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Tag" {
		t.Errorf("expected Dependencies [Tag], got %v", outputs[0].Dependencies)
	}
}

// TestGenerate_ArrayOfMapWrapsCorrectly guards that a map nested inside
// an array needs no extra parenthesization: Record<...>[] is unambiguous
// (Record<...> is a single generic type reference, unlike a bare union).
func TestGenerate_ArrayOfMapWrapsCorrectly(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "ManyMetadata",
				Type: &ir.Node{
					Kind:  ir.KindArray,
					Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}}}},
				},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type ManyMetadata = Record<string, string>[];\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

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

func TestGenerate_InlineObjectWithExtendsRendersIntersection(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Envelope",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{
								Name: "payload",
								Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
									Extends: []string{"Base"},
									Fields: []*ir.Field{
										{Name: "extra", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
									},
								}},
								Optional: true,
							},
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
	want := "export interface Envelope {\n  payload?: Base & { extra?: string; };\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Base" {
		t.Errorf("expected Dependencies [Base], got %v", outputs[0].Dependencies)
	}
}

func TestGenerate_InlineObjectWithExtendsOnlyOmitsEmptyBraces(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Envelope",
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{
								Name:     "payload",
								Type:     &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Extends: []string{"Base", "Other"}}},
								Optional: true,
							},
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
	want := "export interface Envelope {\n  payload?: Base & Other;\n}\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

func TestGenerate_ArrayOfInlineObjectWithExtendsWrapsInParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Items",
				Type: &ir.Node{
					Kind: ir.KindArray,
					Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
						Extends: []string{"Base"},
						Fields: []*ir.Field{
							{Name: "extra", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
						},
					}}},
				},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Items = (Base & { extra?: string; })[];\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
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

// TestGenerate_ArrayOfMultiValueEnumWrapsInParens guards TypeScript's
// operator precedence: postfix [] binds tighter than |, so an array of a
// multi-value enum must be parenthesized. Before the fix, this rendered
// as `"red" | "green"[]`, which parses as `"red" | ("green"[])` — a
// completely different type than the intended `("red" | "green")[]`.
func TestGenerate_ArrayOfMultiValueEnumWrapsInParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Colors",
				Type: &ir.Node{
					Kind:  ir.KindArray,
					Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"red", "green"}}}},
				},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Colors = (\"red\" | \"green\")[];\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

// TestGenerate_ArrayOfSingleValueEnumOmitsParens guards the companion
// case: a single-value enum has no internal | and needs no parens.
func TestGenerate_ArrayOfSingleValueEnumOmitsParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "OnlyRed",
				Type: &ir.Node{
					Kind:  ir.KindArray,
					Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"red"}}}},
				},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type OnlyRed = \"red\"[];\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

// TestGenerate_IntersectionWithMultiValueEnumWrapsInParens guards the
// other unsafe combinator: & binds tighter than |, so
// `Base & "a" | "b"` parses as `(Base & "a") | "b"`, not the intended
// `Base & ("a" | "b")`.
func TestGenerate_IntersectionWithMultiValueEnumWrapsInParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Thing",
				Type: &ir.Node{Kind: ir.KindIntersection, Intersection: &ir.IntersectionNode{Members: []*ir.Node{
					{Kind: ir.KindRef, RefName: "Base"},
					{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"a", "b"}}},
				}}},
			},
		},
	}
	outputs, err := ts.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export type Thing = Base & (\"a\" | \"b\");\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}
