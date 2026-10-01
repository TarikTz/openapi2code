// internal/gen/python/generate_test.go
package python

import (
	"strings"
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

func TestGenerate_DataclassRequiredFieldAfterOptionalUsesKwOnly(t *testing.T) {
	// a declared (iteration-order) first but optional; b declared second
	// but required — a plain @dataclass would be a SyntaxError here
	// ("non-default argument 'b' follows default argument"); kw_only=True
	// must be present on the class to make this valid regardless of order.
	model := &ir.Model{
		Name: "Widget",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "a", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: true},
			{Name: "b", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("got %d outputs, want 1", len(outputs))
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, "@dataclass(kw_only=True)") {
		t.Errorf("declaration missing kw_only=True:\n%s", decl)
	}
	if !strings.Contains(decl, "a: str | None = None") || !strings.Contains(decl, "b: str") {
		t.Errorf("declaration missing expected fields:\n%s", decl)
	}
}

func TestGenerate_PydanticAliasForSanitizedFieldName(t *testing.T) {
	model := &ir.Model{
		Name: "Widget",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "class", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Pydantic)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, `class_: str = Field(alias="class")`) {
		t.Errorf("declaration missing aliased field:\n%s", decl)
	}
	if !strings.Contains(decl, "model_config = ConfigDict(populate_by_name=True)") {
		t.Errorf("declaration missing model_config:\n%s", decl)
	}
	if !containsImport(outputs[0].Imports, "from pydantic import BaseModel, ConfigDict, Field") {
		t.Errorf("imports missing pydantic alias imports: %v", outputs[0].Imports)
	}
}

func TestGenerate_FieldNameCollisionUniquifies(t *testing.T) {
	// "photo_urls" and "photoUrls" both sanitize to "photo_urls".
	model := &ir.Model{
		Name: "Pet",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "photo_urls", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
			{Name: "photoUrls", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, "photo_urls: str") || !strings.Contains(decl, "photo_urls_2: str") {
		t.Errorf("declaration missing both uniquified fields:\n%s", decl)
	}
}

func TestGenerate_InlineObjectFieldBecomesDependency(t *testing.T) {
	model := &ir.Model{
		Name: "Pet",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "owner", Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
				{Name: "name", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
			}}}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(outputs) != 2 {
		t.Fatalf("got %d outputs, want 2 (Pet + synthesized PetOwner)", len(outputs))
	}
	pet := outputs[0]
	if len(pet.Dependencies) != 1 || pet.Dependencies[0] != "PetOwner" {
		t.Errorf("Pet.Dependencies = %v, want [PetOwner]", pet.Dependencies)
	}
}

func TestGenerate_UnionFieldSkippedWithComment(t *testing.T) {
	model := &ir.Model{
		Name: "Pet",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "weird", Type: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{}}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(outputs[0].Declaration, "# weird: skipped") {
		t.Errorf("expected a skipped-field comment for weird:\n%s", outputs[0].Declaration)
	}
}

func TestGenerate_FieldNameCollisionUniquifies_Pydantic(t *testing.T) {
	// Same collision as the dataclass test above; in Pydantic the
	// suffixed field must also keep its wire name via an alias.
	model := &ir.Model{
		Name: "Pet",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: "photo_urls", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
			{Name: "photoUrls", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		}}},
	}
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Pydantic)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, "    photo_urls: str\n") || !strings.Contains(decl, `photo_urls_2: str = Field(alias="photoUrls")`) {
		t.Errorf("declaration missing both uniquified fields:\n%s", decl)
	}
}

func stringField(name string, optional bool) *ir.Field {
	return &ir.Field{Name: name, Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}, Optional: optional}
}

func objectModel(name string, fields ...*ir.Field) *ir.Model {
	return &ir.Model{Name: name, Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: fields}}}
}

// Pydantic v2 treats an underscore-prefixed class attribute as a private
// attribute, not a field — "_id" would silently vanish from the model.
func TestGenerate_PydanticUnderscoreFieldGetsPublicAliasedName(t *testing.T) {
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{objectModel("Doc", stringField("_id", false))}}, Pydantic)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, `    id: str = Field(alias="_id")`) {
		t.Errorf("expected _id rendered as public field id with alias:\n%s", decl)
	}
	if !strings.Contains(decl, "model_config = ConfigDict(populate_by_name=True)") {
		t.Errorf("declaration missing model_config:\n%s", decl)
	}
	for _, line := range strings.Split(decl, "\n") {
		if strings.HasPrefix(line, "    _") {
			t.Errorf("Pydantic declaration has an underscore-prefixed attribute %q:\n%s", line, decl)
		}
	}
}

func TestGenerate_PydanticUnderscoreFieldYieldsToPlainName(t *testing.T) {
	// "_id" sorts (and so resolves) before "id"; "id" must still keep its
	// own unaliased name, with the rewritten "_id" taking the suffix.
	model := objectModel("Doc", stringField("_id", false), stringField("id", true))
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Pydantic)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	if !strings.Contains(decl, `    id_2: str = Field(alias="_id")`) || !strings.Contains(decl, "    id: str | None = None\n") {
		t.Errorf("unexpected collision resolution:\n%s", decl)
	}
}

func TestGenerate_PydanticDigitLeadingAndAllUnderscoreFields(t *testing.T) {
	// "2fa" sanitizes to "_2fa"; stripping leaves a digit-led name.
	// "__" strips to nothing at all.
	model := objectModel("Doc", stringField("2fa", false), stringField("__", false), stringField("_class", false))
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, Pydantic)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := outputs[0].Declaration
	for _, want := range []string{
		`    field_2fa: str = Field(alias="2fa")`,
		`    field: str = Field(alias="__")`,
		`    class_: str = Field(alias="_class")`,
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("declaration missing %q:\n%s", want, decl)
		}
	}
}

func TestGenerate_DataclassKeepsUnderscoreField(t *testing.T) {
	// The private-attribute rule is Pydantic's; a dataclass field named
	// _id is an ordinary field and keeps the wire name unchanged.
	outputs, err := Generate(&ir.Document{Models: []*ir.Model{objectModel("Doc", stringField("_id", false))}}, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(outputs[0].Declaration, "    _id: str\n") {
		t.Errorf("expected dataclass field _id unchanged:\n%s", outputs[0].Declaration)
	}
}

func TestGenerate_SkippedFieldCommentCannotBreakOutOfComment(t *testing.T) {
	wireName := "x\nimport os; os.system('pwned')\r\n\u2028y\u2029z"
	model := &ir.Model{
		Name: "Pet",
		Type: &ir.Node{Kind: ir.KindObject, Object: &ir.ObjectNode{Fields: []*ir.Field{
			{Name: wireName, Type: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{}}},
		}}},
	}
	for _, style := range []Style{Dataclass, Pydantic} {
		outputs, err := Generate(&ir.Document{Models: []*ir.Model{model}}, style)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		decl := outputs[0].Declaration
		if strings.ContainsAny(decl, "\r\u2028\u2029") {
			t.Errorf("style %v: declaration contains a raw line separator:\n%q", style, decl)
		}
		// Every line of the class body is either the header, `pass`, or
		// the one skipped-field comment; the payload must stay inside it.
		var commentLines int
		for _, line := range strings.Split(strings.TrimRight(decl, "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "#"):
				commentLines++
				if !strings.Contains(trimmed, "import os") || !strings.Contains(trimmed, ": skipped") {
					t.Errorf("style %v: unexpected comment line %q", style, line)
				}
			case strings.HasPrefix(trimmed, "class "), strings.HasPrefix(trimmed, "@dataclass"), trimmed == "pass":
			default:
				t.Errorf("style %v: unexpected non-comment line %q in:\n%s", style, line, decl)
			}
		}
		if commentLines != 1 {
			t.Errorf("style %v: got %d comment lines, want exactly 1:\n%s", style, commentLines, decl)
		}
	}
}

func TestCommentSafe(t *testing.T) {
	cases := map[string]string{
		"plain_name":     "plain_name",
		"a\nb":           "a b",
		"a\r\nb":         "a  b",
		"a\u2028b\u2029": "a b ",
		"a\x00b\u0085c":  "a b c",
		"café — ok":      "café — ok",
	}
	for in, want := range cases {
		if got := commentSafe(in); got != want {
			t.Errorf("commentSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerate_AliasFlagMarksOnlyAssignments(t *testing.T) {
	pet := objectModel("Pet", stringField("name", false))
	doc := &ir.Document{Models: []*ir.Model{
		{Name: "AllPets", Type: &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: &ir.Node{Kind: ir.KindRef, RefName: "Pet"}}}},
		{Name: "Color", Type: &ir.Node{Kind: ir.KindEnum, Enum: &ir.EnumNode{Values: []string{"red"}, Primitive: ir.PrimitiveString}}},
		{Name: "Id", Type: &ir.Node{Kind: ir.KindPrimitive, Primitive: ir.PrimitiveString}},
		{Name: "PetByName", Type: &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: &ir.Node{Kind: ir.KindRef, RefName: "Pet"}}}},
		pet,
		{Name: "PetRef", Type: &ir.Node{Kind: ir.KindRef, RefName: "Pet"}},
		{Name: "Weird", Type: &ir.Node{Kind: ir.KindUnion, Union: &ir.UnionNode{}}},
	}}
	outputs, err := Generate(doc, Dataclass)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := map[string]bool{"AllPets": true, "Color": false, "Id": true, "PetByName": true, "Pet": false, "PetRef": true, "Weird": false}
	for _, o := range outputs {
		if o.Alias != want[o.Name] {
			t.Errorf("%s: Alias = %v, want %v (declaration %q)", o.Name, o.Alias, want[o.Name], o.Declaration)
		}
		// Only the comment-only declaration defines no Python name.
		if wantUnsupported := o.Name == "Weird"; o.Unsupported != wantUnsupported {
			t.Errorf("%s: Unsupported = %v, want %v", o.Name, o.Unsupported, wantUnsupported)
		}
	}
}

func containsImport(imports []string, want string) bool {
	for _, imp := range imports {
		if imp == want {
			return true
		}
	}
	return false
}
