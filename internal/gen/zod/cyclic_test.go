package zod_test

import (
	"strings"
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
		"export const ASchema: z.ZodType<A> = z.lazy(() => z.intersection(BSchema, z.object({ extra: z.string().optional(), })));\n"
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

func TestGenerate_NonCyclicExtendsOnlyCyclic(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Base", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "id", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveNumber}, Optional: false},
					{Name: "self", Type: &ir.Node{Kind: ir.KindRef, RefName: "Base"}, Optional: true},
				}}},
			},
			{
				Name: "Derived", Cyclic: false,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
					Extends: []string{"Base"},
					Fields: []*ir.Field{
						{Name: "name", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
					},
				}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantDerived := "export const DerivedSchema = z.intersection(BaseSchema, z.object({ name: z.string(), }));\n" +
		"export type Derived = z.infer<typeof DerivedSchema>;\n"
	if outputs[1].Declaration != wantDerived {
		t.Errorf("Derived got:\n%s\nwant:\n%s", outputs[1].Declaration, wantDerived)
	}
	if len(outputs[1].Dependencies) != 1 || outputs[1].Dependencies[0] != "Base" {
		t.Errorf("Derived deps: got %v, want [Base]", outputs[1].Dependencies)
	}
}

// TestGenerate_TransitiveExtendsOfCyclicBase covers the three-model chain
// Base (cyclic) <- Mid <- Leaf. Mid extends a cyclic base, so MidSchema is
// a z.intersection, which has no .extend() method — Leaf must therefore
// fall back to z.intersection too, even though Mid itself is not cyclic.
func TestGenerate_TransitiveExtendsOfCyclicBase(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Base", Cyclic: true,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
					{Name: "id", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
					{Name: "self", Type: &ir.Node{Kind: ir.KindRef, RefName: "Base"}, Optional: true},
				}}},
			},
			{
				Name: "Mid", Cyclic: false,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
					Extends: []string{"Base"},
					Fields: []*ir.Field{
						{Name: "mid", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
					},
				}},
			},
			{
				Name: "Leaf", Cyclic: false,
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
					Extends: []string{"Mid"},
					Fields: []*ir.Field{
						{Name: "leaf", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
					},
				}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	byName := map[string]zod.ModelOutput{}
	for _, o := range outputs {
		byName[o.Name] = o
	}

	wantMid := "export const MidSchema = z.intersection(BaseSchema, z.object({ mid: z.string(), }));\n" +
		"export type Mid = z.infer<typeof MidSchema>;\n"
	if got := byName["Mid"].Declaration; got != wantMid {
		t.Errorf("Mid got:\n%s\nwant:\n%s", got, wantMid)
	}

	wantLeaf := "export const LeafSchema = z.intersection(MidSchema, z.object({ leaf: z.string(), }));\n" +
		"export type Leaf = z.infer<typeof LeafSchema>;\n"
	leaf := byName["Leaf"].Declaration
	if leaf != wantLeaf {
		t.Errorf("Leaf got:\n%s\nwant:\n%s", leaf, wantLeaf)
	}
	if strings.Contains(leaf, ".extend(") {
		t.Errorf("Leaf must not call .extend() on a ZodIntersection, got:\n%s", leaf)
	}
	if strings.Contains(leaf, "MidSchema.shape") {
		t.Errorf("Leaf must not read .shape off a ZodIntersection, got:\n%s", leaf)
	}
}

// TestGenerate_ExtendsNonObjectModel covers an allOf whose base model is
// not object-shaped (here an enum). z.enum(...) has no .extend() either,
// so this must also fold to an intersection.
func TestGenerate_ExtendsNonObjectModel(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name: "Kind",
				Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"a", "b"}}},
			},
			{
				Name: "Tagged",
				Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{
					Extends: []string{"Kind"},
					Fields: []*ir.Field{
						{Name: "label", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: false},
					},
				}},
			},
		},
	}
	outputs, err := zod.Generate(doc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "export const TaggedSchema = z.intersection(KindSchema, z.object({ label: z.string(), }));\n" +
		"export type Tagged = z.infer<typeof TaggedSchema>;\n"
	if got := outputs[1].Declaration; got != want {
		t.Errorf("Tagged got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerate_CyclicWithComplexFields(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name:   "Complex",
				Cyclic: true,
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "refs", Type: &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindRef, RefName: "Complex"}}}, Optional: true},
							{Name: "status", Type: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{Variants: []*ir.Node{
								{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString},
								{Kind: ir.KindRef, RefName: "Complex"},
							}}}, Optional: true, Nullable: true},
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
	// Check that TS type is present and well-formed with array and union support
	if !strings.Contains(outputs[0].Declaration, "export type Complex = {") {
		t.Errorf("Complex TS type malformed: got:\n%s", outputs[0].Declaration)
	}
	if !strings.Contains(outputs[0].Declaration, "refs?:") {
		t.Errorf("refs field missing from TS type")
	}
	if !strings.Contains(outputs[0].Declaration, "status?:") {
		t.Errorf("status field missing from TS type")
	}
}

// TestGenerate_CyclicArrayOfMultiValueEnumWrapsInParens guards
// wrapForTSCombinator the same way ts/generate_test.go's
// TestGenerate_ArrayOfMultiValueEnumWrapsInParens guards its non-cyclic
// counterpart: TypeScript's postfix [] binds tighter than |, so an array
// of a multi-value enum needs parens in a cyclic model's hand-written TS
// type annotation too. Before the fix this rendered as
// `"red" | "green"[]`, which parses as a completely different type.
func TestGenerate_CyclicArrayOfMultiValueEnumWrapsInParens(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name:   "Complex",
				Cyclic: true,
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "self", Type: &ir.Node{Kind: ir.KindRef, RefName: "Complex"}, Optional: true},
							{Name: "colors", Type: &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"red", "green"}}}}}, Optional: true},
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
	if !strings.Contains(outputs[0].Declaration, `("red" | "green")[]`) {
		t.Errorf("expected parenthesized enum array in TS type, got:\n%s", outputs[0].Declaration)
	}
}

// TestGenerate_CyclicMapField covers renderTSType's KindMap case (a
// self-contained TS-type-text renderer, separate from the plain TS
// generator, used only for a cyclic model's z.ZodType<X> annotation).
func TestGenerate_CyclicMapField(t *testing.T) {
	doc := &ir.Document{
		Models: []*ir.Model{
			{
				Name:   "Complex",
				Cyclic: true,
				Type: &ir.Node{
					Kind: ir.KindObject,
					Object: &ir.ObjectNode{
						Fields: []*ir.Field{
							{Name: "self", Type: &ir.Node{Kind: ir.KindRef, RefName: "Complex"}, Optional: true},
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
	if !strings.Contains(outputs[0].Declaration, "Record<string, string>") {
		t.Errorf("expected a Record<string, string> TS type, got:\n%s", outputs[0].Declaration)
	}
	if !strings.Contains(outputs[0].Declaration, "z.record(z.string())") {
		t.Errorf("expected a z.record(z.string()) zod schema, got:\n%s", outputs[0].Declaration)
	}
}
