// internal/gen/python/generate_test.go
package python

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

func newTestRenderer(models ...*ir.Model) *renderer {
	doc := &ir.Document{Models: models}
	names := mobile.AssignNames(doc.Models, SanitizeTypeIdentifier)
	used := make(map[string]bool, len(names))
	for _, n := range names {
		used[n] = true
	}
	modelsByName := make(map[string]*ir.Model, len(models))
	for _, m := range models {
		modelsByName[m.Name] = m
	}
	return &renderer{names: names, modelsByName: modelsByName, used: used, style: Dataclass}
}

func TestResolveType_Primitives(t *testing.T) {
	r := newTestRenderer()
	cases := []struct {
		prim ir.Primitive
		want string
	}{
		{ir.PrimitiveString, "str"},
		{ir.PrimitiveNumber, "float"},
		{ir.PrimitiveInteger, "int"},
		{ir.PrimitiveBoolean, "bool"},
	}
	for _, c := range cases {
		res, _ := r.resolveType(&ir.Node{Kind: ir.KindPrimitive, Primitive: c.prim}, "Field")
		if res.Skip || res.Expr != c.want {
			t.Errorf("primitive %v: got expr=%q skip=%v, want %q", c.prim, res.Expr, res.Skip, c.want)
		}
	}
}

func TestResolveType_UnknownPrimitiveSkipped(t *testing.T) {
	r := newTestRenderer()
	res, _ := r.resolveType(&ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveUnknown}, "Field")
	if !res.Skip {
		t.Error("expected PrimitiveUnknown to be skipped")
	}
}

func TestResolveType_Array(t *testing.T) {
	r := newTestRenderer()
	node := &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}}}
	res, _ := r.resolveType(node, "Field")
	if res.Skip || res.Expr != "list[str]" {
		t.Errorf("got %q, want list[str]", res.Expr)
	}
}

func TestResolveType_Map(t *testing.T) {
	r := newTestRenderer()
	node := &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveInteger}}}
	res, _ := r.resolveType(node, "Field")
	if res.Skip || res.Expr != "dict[str, int]" {
		t.Errorf("got %q, want dict[str, int]", res.Expr)
	}
}

func TestResolveType_Ref(t *testing.T) {
	pet := &ir.Model{Name: "Pet", Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{}}}
	r := newTestRenderer(pet)
	res, _ := r.resolveType(&ir.Node{Kind: ir.KindRef, RefName: "Pet"}, "Field")
	if res.Skip || res.Expr != "Pet" || len(res.Deps) != 1 || res.Deps[0] != "Pet" {
		t.Errorf("got expr=%q deps=%v skip=%v", res.Expr, res.Deps, res.Skip)
	}
}

func TestResolveType_InlineObjectSynthesizesDecl(t *testing.T) {
	r := newTestRenderer()
	node := &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{{Name: "id", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}}}}}
	res, extra := r.resolveType(node, "PetOwner")
	if res.Skip || res.Expr != "PetOwner" {
		t.Errorf("got expr=%q skip=%v, want PetOwner", res.Expr, res.Skip)
	}
	if len(extra) != 1 || extra[0].Name != "PetOwner" {
		t.Errorf("got extra=%v, want one PendingDecl named PetOwner", extra)
	}
	if len(res.Deps) != 1 || res.Deps[0] != "PetOwner" {
		t.Errorf("got deps=%v, want [PetOwner]", res.Deps)
	}
}

func TestResolveType_BooleanEnumBypassesSynthesis(t *testing.T) {
	r := newTestRenderer()
	node := &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"true", "false"}, Primitive: ir.PrimitiveBoolean}}
	res, extra := r.resolveType(node, "Flag")
	if res.Skip || res.Expr != "bool" || len(extra) != 0 {
		t.Errorf("got expr=%q skip=%v extra=%v, want bool with no synthesized decl", res.Expr, res.Skip, extra)
	}
}

func TestResolveType_UnionSkipped(t *testing.T) {
	r := newTestRenderer()
	res, _ := r.resolveType(&ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{}}, "Field")
	if !res.Skip {
		t.Error("expected KindUnion to be skipped")
	}
}
