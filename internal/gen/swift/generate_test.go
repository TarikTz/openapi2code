package swift_test

import (
	"os"
	"os/exec"
	"path/filepath"
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

// assertSwiftCompiles hands decls to the real Swift compiler
// (`swiftc -typecheck`), since a string-contains assertion cannot tell
// whether generated Swift actually compiles (multiple bugs in this
// generator were only ever caught this way — see the task-2 fix report).
// It skips (never fails) when swiftc isn't on PATH, which is an
// environment limitation, not a generator defect.
func assertSwiftCompiles(t *testing.T, decls ...string) {
	t.Helper()
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("swiftc not found on PATH; skipping real-compiler verification")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "generated.swift")
	content := strings.Join(decls, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write swift source: %v", err)
	}
	cmd := exec.Command(swiftc, "-typecheck", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("generated Swift does not compile (swiftc -typecheck):\n%s\n--- source ---\n%s", out, content)
	}
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
	if strings.Contains(decl, "public var weird") {
		t.Errorf("expected the union-typed field to be omitted as a real property, got:\n%s", decl)
	}
	if !strings.Contains(decl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"weird\" was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public var name: String") {
		t.Errorf("expected the model's other field to still be generated, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
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
	if len(models) != 2 {
		t.Fatalf("expected both Pet and a comment-only StringOrNumber declaration, got: %+v", models)
	}
	var petDecl, stringOrNumberDecl string
	for _, m := range models {
		switch m.Name {
		case "Pet":
			petDecl = m.Declaration
		case "StringOrNumber":
			stringOrNumberDecl = m.Declaration
		}
	}
	if petDecl == "" {
		t.Fatalf("expected Pet to still be generated, got: %+v", models)
	}
	if stringOrNumberDecl == "" {
		t.Fatalf("expected a comment-only StringOrNumber declaration, got: %+v", models)
	}
	if !strings.Contains(stringOrNumberDecl, "// StringOrNumber was not generated for the Swift target: unsupported schema shape") {
		t.Errorf("expected a comment explaining why StringOrNumber was skipped, got:\n%s", stringOrNumberDecl)
	}
	if strings.Contains(stringOrNumberDecl, "struct") || strings.Contains(stringOrNumberDecl, "enum") || strings.Contains(stringOrNumberDecl, "class") {
		t.Errorf("expected StringOrNumber's declaration to be comment-only, got:\n%s", stringOrNumberDecl)
	}
	assertSwiftCompiles(t, petDecl, stringOrNumberDecl)
}

// TestGenerate_EmptyObjectHasNoCodingKeysAndCompiles pins Critical Finding
// 1 from task-2 review: a struct with zero renderable fields (here, a
// bare `{type: object}` with no properties at all) must not emit an empty
// CodingKeys raw-value enum — swiftc rejects "an enum with no cases
// cannot declare a raw type" — and Swift's default Codable synthesis
// handles a zero-property struct on its own.
func TestGenerate_EmptyObjectHasNoCodingKeysAndCompiles(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Empty": {Type: "object"},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "CodingKeys") {
		t.Errorf("expected no CodingKeys enum for a struct with zero fields, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public struct Empty: Codable {") {
		t.Errorf("expected an Empty struct, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_ObjectWithOnlySkippedFieldHasNoCodingKeysAndCompiles covers
// the same Critical Finding 1 gap reached a different way: every property
// is present but gets skipped as an unsupported shape, so the struct ends
// up with zero renderable fields even though it started with one.
func TestGenerate_ObjectWithOnlySkippedFieldHasNoCodingKeysAndCompiles(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Weird": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"value": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "CodingKeys") {
		t.Errorf("expected no CodingKeys enum when every field was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "// value: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"value\" was skipped, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_CyclicModelWithAllFieldsSkippedCompiles covers Critical
// Finding 1's class branch: a cyclic model (so it renders as a class with
// a hand-written init(from:)) whose only field is skipped as an
// unsupported shape. The class must still get a valid parameterless
// init() and an init(from:) that reads nothing, rather than a
// container-based init(from:) with nothing to decode.
func TestGenerate_CyclicModelWithAllFieldsSkippedCompiles(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Weird": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"self": {
						OneOf: []*spec.RawSchema{
							{Ref: "#/components/schemas/Weird"},
							{Type: "string"},
						},
					},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "public class Weird: Codable {") {
		t.Fatalf("expected Weird to be cyclic (a class) via its self-referencing oneOf, got:\n%s", decl)
	}
	if strings.Contains(decl, "CodingKeys") {
		t.Errorf("expected no CodingKeys enum when every field was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public init() {}") {
		t.Errorf("expected a parameterless init() for a class with zero fields, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public required init(from decoder: Decoder) throws {}") {
		t.Errorf("expected an empty init(from:) for a class with zero fields, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_TopLevelArrayOfInlineObjectDoesNotSelfReference pins
// Critical Finding 2's reproducible case: a top-level model whose entire
// type is an array of an inline (non-$ref) object must not synthesize
// its item type under the model's own name — that produced the
// self-referential, non-compiling `public typealias Tags = [Tags]`.
func TestGenerate_TopLevelArrayOfInlineObjectDoesNotSelfReference(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Tags": {
				Type: "array",
				Items: &spec.RawSchema{
					Type: "object",
					Properties: map[string]*spec.RawSchema{
						"label": {Type: "string"},
					},
					Required: []string{"label"},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 declarations (the Tags typealias + its synthesized item type), got %d: %+v", len(models), models)
	}
	var tagsDecl, itemDecl, itemName string
	for _, m := range models {
		if m.Name == "Tags" {
			tagsDecl = m.Declaration
		} else {
			itemName = m.Name
			itemDecl = m.Declaration
		}
	}
	if tagsDecl == "" {
		t.Fatalf("expected a Tags declaration, got: %+v", models)
	}
	if itemName == "Tags" {
		t.Fatalf("expected the synthesized item type to have a name distinct from Tags, got: %+v", models)
	}
	if strings.Contains(tagsDecl, "[Tags]") {
		t.Fatalf("expected Tags to not self-reference, got:\n%s", tagsDecl)
	}
	if !strings.Contains(tagsDecl, "public typealias Tags = ["+itemName+"]") {
		t.Errorf("expected Tags to alias to an array of %s, got:\n%s", itemName, tagsDecl)
	}
	assertSwiftCompiles(t, tagsDecl, itemDecl)
}

// TestGenerate_SynthesizedNameCollisionWithExistingModelGetsUniqueName
// pins Critical Finding 2's silent case: an inline field's suggested
// synthesized name can collide with an unrelated top-level model's ACTUAL
// name. Before the fix, the field's type expression and the synthesized
// declaration's enqueued name were computed separately and could
// disagree — the field would silently point at the wrong (unrelated)
// model while the real synthesized type was stranded under an
// unreferenced "_2"-suffixed name.
func TestGenerate_SynthesizedNameCollisionWithExistingModelGetsUniqueName(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			// An unrelated top-level model whose name is exactly what
			// User's inline "profile" field would suggest for itself
			// ("User" + PascalCase("profile") == "UserProfile").
			"UserProfile": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"bio": {Type: "string"}},
				Required:   []string{"bio"},
			},
			"User": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"profile": {
						Type:       "object",
						Properties: map[string]*spec.RawSchema{"age": {Type: "integer"}},
						Required:   []string{"age"},
					},
				},
				Required: []string{"profile"},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	byName := make(map[string]string, len(models))
	for _, m := range models {
		if _, dup := byName[m.Name]; dup {
			t.Fatalf("duplicate declaration name %q in output: %+v", m.Name, models)
		}
		byName[m.Name] = m.Declaration
	}
	if len(models) != 3 {
		t.Fatalf("expected exactly 3 declarations (UserProfile, User, and one disambiguated synthesized type — no orphaned extra declaration), got %d: %+v", len(models), models)
	}

	userProfileDecl, ok := byName["UserProfile"]
	if !ok {
		t.Fatalf("expected the real UserProfile model to still be generated, got: %+v", models)
	}
	if !strings.Contains(userProfileDecl, "public var bio: String") {
		t.Errorf("expected the real UserProfile to keep its own field, got:\n%s", userProfileDecl)
	}

	userDecl, ok := byName["User"]
	if !ok {
		t.Fatalf("expected User to be generated, got: %+v", models)
	}

	const disambiguated = "UserProfile_2"
	synthesizedDecl, ok := byName[disambiguated]
	if !ok {
		t.Fatalf("expected a disambiguated %q declaration for the colliding inline object, got: %+v", disambiguated, models)
	}
	if !strings.Contains(userDecl, "public var profile: "+disambiguated) {
		t.Errorf("expected User.profile to reference the disambiguated synthesized type %q, not the unrelated UserProfile model, got:\n%s", disambiguated, userDecl)
	}
	if !strings.Contains(synthesizedDecl, "public var age: Int") {
		t.Errorf("expected the synthesized type to have the inline object's own field, got:\n%s", synthesizedDecl)
	}

	assertSwiftCompiles(t, userProfileDecl, userDecl, synthesizedDecl)
}

// TestGenerate_FlattenFields_OwnFieldOverridesBaseField pins Important
// Finding 3: when a derived (allOf) schema redeclares a field its base
// also declares, the derived schema's own field must win — matching
// internal/gen/ts's `extends` (where the subtype's own declaration
// shadows the base's) and internal/gen/zod's `.extend()` (whose
// own-fields object is merged in last).
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
	models, err := swift.Generate(buildDoc(t, raw))
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
	if !strings.Contains(dogDecl, "public var name: DogName") {
		t.Errorf("expected Dog.name to use the overriding DogName type, got:\n%s", dogDecl)
	}
	if strings.Contains(dogDecl, "public var name: String") {
		t.Errorf("expected Dog.name to NOT fall back to the base Animal's String type, got:\n%s", dogDecl)
	}
}

// TestGenerate_TransitiveAllOfOwnFieldOverrideSurvivesGrandchild extends
// TestGenerate_FlattenFields_OwnFieldOverridesBaseField by one more allOf
// level. Before the fix, flattenExtendsTarget applied own-wins dedup only
// at the outermost model — a grandchild silently reverted to the
// grandparent's field type instead of the parent's override, so
// Dog.name and Puppy.name disagreed on the same logical field within one
// generated file.
func TestGenerate_TransitiveAllOfOwnFieldOverrideSurvivesGrandchild(t *testing.T) {
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
					{Type: "object", Properties: map[string]*spec.RawSchema{"name": {Ref: "#/components/schemas/DogName"}}},
				},
			},
			"Puppy": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Dog"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"age": {Type: "integer"}}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var puppyDecl string
	allDecls := make([]string, 0, len(models))
	for _, m := range models {
		allDecls = append(allDecls, m.Declaration)
		if m.Name == "Puppy" {
			puppyDecl = m.Declaration
		}
	}
	if puppyDecl == "" {
		t.Fatalf("expected a Puppy declaration, got: %+v", models)
	}
	if !strings.Contains(puppyDecl, "public var name: DogName") {
		t.Errorf("expected Puppy.name to inherit Dog's DogName override, got:\n%s", puppyDecl)
	}
	if strings.Contains(puppyDecl, "public var name: String") {
		t.Errorf("expected Puppy.name to NOT revert to Animal's String type, got:\n%s", puppyDecl)
	}
	assertSwiftCompiles(t, allDecls...)
}

// TestGenerate_AllOfBaseCollapsedToRefAliasStillContributesFields covers
// the "wrap a $ref in allOf just to attach a description" idiom, which
// ir.Build collapses a single-ref, no-own-properties allOf down to a
// bare KindRef alias (see TestBuild_SingleRefAllOfCollapsesToRef in
// internal/ir). Before the fix, flattenExtendsTarget bailed out with nil
// the moment an Extends target's Kind was KindRef instead of KindObject,
// silently dropping every field the alias's underlying object declares —
// with no comment, unlike every other unsupported-shape case.
func TestGenerate_AllOfBaseCollapsedToRefAliasStillContributesFields(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Identifiable": {Type: "object", Properties: map[string]*spec.RawSchema{"id": {Type: "string"}}, Required: []string{"id"}},
			"Base": {
				AllOf: []*spec.RawSchema{{Ref: "#/components/schemas/Identifiable"}},
			},
			"Dog": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Base"},
					{Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
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
	if !strings.Contains(dogDecl, "public var id: String") {
		t.Errorf("expected Dog to inherit 'id' through the Base alias, got:\n%s", dogDecl)
	}
	if !strings.Contains(dogDecl, "public var name: String") {
		t.Errorf("expected Dog to still have its own 'name' field, got:\n%s", dogDecl)
	}
	assertSwiftCompiles(t, dogDecl)
}

// TestGenerate_RefToSkippedModelIsOmittedWithComment covers a field that
// $refs a model which itself renders as a comment-only unsupported-shape
// declaration (here a bare oneOf). Before the fix, resolveType's KindRef
// case never checked what the target actually rendered as, so the field
// referenced a Swift type that was never declared — swiftc: "cannot find
// type 'StringOrNumber' in scope".
func TestGenerate_RefToSkippedModelIsOmittedWithComment(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Pet": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"weird": {Ref: "#/components/schemas/StringOrNumber"}},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl, stringOrNumberDecl string
	for _, m := range models {
		switch m.Name {
		case "Pet":
			petDecl = m.Declaration
		case "StringOrNumber":
			stringOrNumberDecl = m.Declaration
		}
	}
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
	}
	assertSwiftCompiles(t, petDecl, stringOrNumberDecl)
}

// TestGenerate_TypealiasToSkippedModelIsCommentOnly covers the
// declaration-level twin of the field-level bug above: a named model
// that is itself a bare $ref to an unsupported-shape model must also be
// omitted with a comment, not rendered as `public typealias X = Y` when
// Y was never declared.
func TestGenerate_TypealiasToSkippedModelIsCommentOnly(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Alias":          {Ref: "#/components/schemas/StringOrNumber"},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var aliasDecl string
	for _, m := range models {
		if m.Name == "Alias" {
			aliasDecl = m.Declaration
		}
	}
	if aliasDecl == "" {
		t.Fatalf("expected an Alias declaration, got: %+v", models)
	}
	if strings.Contains(aliasDecl, "typealias") {
		t.Errorf("expected Alias to be comment-only, not a typealias to an undeclared type, got:\n%s", aliasDecl)
	}
	if !strings.Contains(aliasDecl, "// Alias was not generated for the Swift target: unsupported schema shape") {
		t.Errorf("expected a skip comment, got:\n%s", aliasDecl)
	}
}

// TestGenerate_MapField covers a field built from additionalProperties
// (ir.KindMap), rendered as Swift's native Dictionary — [String: V] is
// Codable for free whenever V is, so no hand-written serialization is
// needed, unlike Kotlin/Dart.
func TestGenerate_MapField(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"metadata": {Type: "object", AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Type: "string"}, Allowed: true}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "public var metadata: [String: String]?") {
		t.Errorf("expected a [String: String]? map field, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_TopLevelMapOfRef covers a named top-level map schema whose
// values are a $ref, checking the generated typealias compiles alongside
// the referenced type.
func TestGenerate_TopLevelMapOfRef(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Tag": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"TagsByID": {
				Type:                 "object",
				AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Ref: "#/components/schemas/Tag"}, Allowed: true},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var tagsByIDDecl, tagDecl string
	for _, m := range models {
		switch m.Name {
		case "TagsByID":
			tagsByIDDecl = m.Declaration
		case "Tag":
			tagDecl = m.Declaration
		}
	}
	if !strings.Contains(tagsByIDDecl, "public typealias TagsByID = [String: Tag]") {
		t.Errorf("expected a [String: Tag] typealias, got:\n%s", tagsByIDDecl)
	}
	assertSwiftCompiles(t, tagDecl, tagsByIDDecl)
}

// TestGenerate_RefToMapOfSkippedModelIsOmittedWithComment covers the map
// counterpart of TestGenerate_RefToSkippedModelIsOmittedWithComment: a
// field whose map values are $ref to an unsupported-shape model must be
// skipped with a comment too, not reference an undeclared type.
func TestGenerate_RefToMapOfSkippedModelIsOmittedWithComment(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"weird": {Type: "object", AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Ref: "#/components/schemas/StringOrNumber"}, Allowed: true}},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var petDecl string
	for _, m := range models {
		if m.Name == "Pet" {
			petDecl = m.Declaration
		}
	}
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
	}
	assertSwiftCompiles(t, petDecl)
}

// TestGenerate_EmptyStringEnumValueIsNotBareUnderscore pins the
// final-review Critical 1 finding. An `enum: ["", "asc", "desc"]` (a real
// "no sort direction specified" pattern) sanitizes its empty value down to
// "_", which swiftc rejects outright: "keyword '_' cannot be used as an
// identifier here". Reserving "_" and letting the empty-string case fall
// through the same reserved-word check as every other input turns it into
// "__". The raw value must stay the empty string either way.
func TestGenerate_EmptyStringEnumValueIsNotBareUnderscore(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Sort": {Type: "string", Enum: []interface{}{"", "asc", "desc"}},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "case _ =") {
		t.Errorf("expected the empty enum value to not become the bare wildcard `_`, got:\n%s", decl)
	}
	if !strings.Contains(decl, `case __ = ""`) {
		t.Errorf("expected `case __ = \"\"`, got:\n%s", decl)
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_PunctuationOnlyEnumValuesAreNotBareUnderscore covers the
// other route into the same bug: a comparison-operator enum, where each
// value's only character is stripped to "_" by invalidIdentChar (rather
// than the input being empty to begin with). All four values converge on
// "_", so this exercises Critical 1 and Critical 2's enum half together.
func TestGenerate_PunctuationOnlyEnumValuesAreNotBareUnderscore(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Comparison": {Type: "string", Enum: []interface{}{"<", ">", "<=", ">="}},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "case _ =") {
		t.Errorf("expected no bare `_` case, got:\n%s", decl)
	}
	for _, want := range []string{`case __ = "<"`, `case ___2 = ">"`, `case ___3 = "<="`, `case ___4 = ">="`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_CollidingFieldNamesAreUniquified pins the final-review
// Critical 2 finding: two DIFFERENT wire names converging on one camelCase
// identifier used to emit two identically-named properties. Each property
// must keep its OWN wire name in CodingKeys — the part most likely to go
// subtly wrong when uniquifying.
func TestGenerate_CollidingFieldNamesAreUniquified(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"photo_urls": {Type: "string"},
					"photoUrls":  {Type: "string"},
				},
				Required: []string{"photo_urls", "photoUrls"},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Count(decl, "public var photoUrls:") != 1 {
		t.Errorf("expected exactly one `public var photoUrls:`, got:\n%s", decl)
	}
	if !strings.Contains(decl, "public var photoUrls_2:") {
		t.Errorf("expected the colliding second field to be uniquified, got:\n%s", decl)
	}
	for _, want := range []string{`case photoUrls = "photoUrls"`, `case photoUrls_2 = "photo_urls"`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q — each property must map back to its own wire name — in:\n%s", want, decl)
		}
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_CollidingEnumValueNamesAreUniquified is Critical 2's enum
// half: two different wire values converging on one case identifier.
func TestGenerate_CollidingEnumValueNamesAreUniquified(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Kind": {Type: "string", Enum: []interface{}{"photo_urls", "photoUrls"}},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	for _, want := range []string{`case photoUrls = "photo_urls"`, `case photoUrls_2 = "photoUrls"`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
	assertSwiftCompiles(t, decl)
}

// TestGenerate_CollidingInheritedAndOwnFieldNamesAreUniquified checks that
// uniquification covers the flattened field list, so an allOf-inherited
// field colliding with an own field under camelCase is caught too. This is
// distinct from flattenFields' own concern (two fields with the IDENTICAL
// wire name, where the own field wins).
func TestGenerate_CollidingInheritedAndOwnFieldNamesAreUniquified(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Base": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"photo_urls": {Type: "string"}},
				Required:   []string{"photo_urls"},
			},
			"Derived": {
				AllOf: []*spec.RawSchema{
					{Ref: "#/components/schemas/Base"},
					{
						Type:       "object",
						Properties: map[string]*spec.RawSchema{"photoUrls": {Type: "string"}},
						Required:   []string{"photoUrls"},
					},
				},
			},
		},
	}
	models, err := swift.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var derived string
	for _, m := range models {
		if m.Name == "Derived" {
			derived = m.Declaration
		}
	}
	if derived == "" {
		t.Fatalf("expected a Derived declaration, got: %+v", models)
	}
	if strings.Count(derived, "public var photoUrls:") != 1 || !strings.Contains(derived, "public var photoUrls_2:") {
		t.Errorf("expected the inherited and own field to get distinct identifiers, got:\n%s", derived)
	}
	for _, want := range []string{`= "photo_urls"`, `= "photoUrls"`} {
		if !strings.Contains(derived, want) {
			t.Errorf("expected wire name %q to survive uniquification, got:\n%s", want, derived)
		}
	}
	assertSwiftCompiles(t, derived)
}
