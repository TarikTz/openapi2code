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

// TestGenerate_MapField covers a field built from additionalProperties
// (ir.KindMap), rendered as zod@^3's single-argument z.record(valueSchema).
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
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const PetSchema = z.object({ metadata: z.record(z.string()).optional(), });\n" +
		"export type Pet = z.infer<typeof PetSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
}

// TestGenerate_TopLevelMapOfRef covers a named top-level map schema whose
// values are a $ref, checking both the rendered schema/type and that the
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
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const TagsByIDSchema = z.record(TagSchema);\n" +
		"export type TagsByID = z.infer<typeof TagsByIDSchema>;\n"
	if outputs[0].Declaration != want {
		t.Errorf("got:\n%s\nwant:\n%s", outputs[0].Declaration, want)
	}
	if len(outputs[0].Dependencies) != 1 || outputs[0].Dependencies[0] != "Tag" {
		t.Errorf("expected Dependencies [Tag], got %v", outputs[0].Dependencies)
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
