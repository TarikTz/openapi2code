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
		`"photo_urls": photoUrls`,
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
// the same bug class internal/gen/kotlin also guards against: r.kinds is
// keyed by the resolved Dart identifier (see Generate), but a $ref's RefName carries
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
// TestGenerate_MapFieldOfPrimitive covers a field built from
// additionalProperties (ir.KindMap) whose values are a plain primitive.
func TestGenerate_MapFieldOfPrimitive(t *testing.T) {
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
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "final Map<String, String>? metadata;") {
		t.Errorf("expected a Map<String, String>? field, got:\n%s", decl)
	}
	if !strings.Contains(decl, `metadata?.map((key, e) => MapEntry(key, e))`) {
		t.Errorf("expected a per-entry toJson passthrough for a primitive-valued map, got:\n%s", decl)
	}
	if !strings.Contains(decl, `(json["metadata"] as Map<String, dynamic>?)?.map((key, e) => MapEntry(key, e as String))`) {
		t.Errorf("expected a per-entry String cast in fromJson, got:\n%s", decl)
	}
}

// TestGenerate_MapFieldOfRefObject covers a map whose values are a $ref
// to an object model — each entry needs .toJson()/.fromJson() called on
// its VALUE, not its key.
func TestGenerate_MapFieldOfRefObject(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Tag": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Pet": {
				Type:     "object",
				Required: []string{"tagsByID"},
				Properties: map[string]*spec.RawSchema{
					"tagsByID": {Type: "object", AdditionalProperties: &spec.AdditionalProperties{Schema: &spec.RawSchema{Ref: "#/components/schemas/Tag"}, Allowed: true}},
				},
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
	if !strings.Contains(petDecl, "final Map<String, Tag> tagsByID;") {
		t.Errorf("expected a Map<String, Tag> field, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "tagsByID.map((key, e) => MapEntry(key, e.toJson()))") {
		t.Errorf("expected toJson() called per-entry on the map's values, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `(json["tagsByID"] as Map<String, dynamic>).map((key, e) => MapEntry(key, Tag.fromJson(e as Map<String, dynamic>)))`) {
		t.Errorf("expected Tag.fromJson() called per-entry, got:\n%s", petDecl)
	}
}

// TestGenerate_TopLevelMapOfRef covers a named top-level map schema whose
// values are a $ref, checking the generated typedef and its dependency.
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
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var tagsByID *dart.ModelOutput
	for i, m := range models {
		if m.Name == "TagsByID" {
			tagsByID = &models[i]
		}
	}
	if tagsByID == nil {
		t.Fatalf("expected a TagsByID declaration, got: %+v", models)
	}
	if tagsByID.Declaration != "typedef TagsByID = Map<String, Tag>;\n" {
		t.Errorf("got %q", tagsByID.Declaration)
	}
	if len(tagsByID.Dependencies) != 1 || tagsByID.Dependencies[0] != "Tag" {
		t.Errorf("expected Dependencies [Tag], got %v", tagsByID.Dependencies)
	}
}

// TestGenerate_RefToArrayOfInlineObjectFieldIsSkipped guards a field
// whose $ref points at a top-level array alias with an INLINE (non-$ref)
// object item — e.g. `Things: {type: array, items: {type: object, ...}}`.
// Before this fix, such a field still declared its type as the alias
// (`final Things things;`) but generated
// `(json["things"] as List<dynamic>).map((e) => e).toList()` for
// deserialization: this compiles, but every element is left as a raw
// Map<String, dynamic> instead of the typed ThingsItem — a silent
// runtime crash the first time calling code accesses a member on an
// element. The alias itself (Things) still renders correctly; only a
// field referencing it through this exact shape is unrepresentable and
// must be skipped with a comment instead.
func TestGenerate_RefToArrayOfInlineObjectFieldIsSkipped(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Things": {
				Type:  "array",
				Items: &spec.RawSchema{Type: "object", Properties: map[string]*spec.RawSchema{"label": {Type: "string"}}},
			},
			"Container": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"things": {Ref: "#/components/schemas/Things"}},
				Required:   []string{"things"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var containerDecl, thingsDecl string
	for _, m := range models {
		switch m.Name {
		case "Container":
			containerDecl = m.Declaration
		case "Things":
			thingsDecl = m.Declaration
		}
	}
	if strings.Contains(containerDecl, "final Things things") {
		t.Errorf("expected the things field to be omitted as a real property, got:\n%s", containerDecl)
	}
	if !strings.Contains(containerDecl, "// things: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"things\" was skipped, got:\n%s", containerDecl)
	}
	if strings.Contains(containerDecl, "(e) => e") {
		t.Errorf("must not generate the silently-wrong (e) => e dispatch (raw Map, not a typed ThingsItem), got:\n%s", containerDecl)
	}
	if !strings.Contains(thingsDecl, "typedef Things = List<ThingsItem>;") {
		t.Errorf("expected Things itself to still render correctly, got:\n%s", thingsDecl)
	}
}

// TestGenerate_RefToMapOfSkippedModelIsOmittedWithComment covers a field
// whose map values are $ref to an unsupported-shape model — must be
// skipped with a comment, not reference an undeclared type.
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
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
	}
}

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

// TestGenerate_TransitiveAllOfOwnFieldOverrideSurvivesGrandchild extends
// TestGenerate_FlattenFields_OwnFieldOverridesBaseField by one more
// allOf level. Before the fix, flattenExtendsTarget applied own-wins
// dedup only at the outermost model — a grandchild silently reverted to
// the grandparent's field type instead of the parent's override, so
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
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var puppyDecl string
	for _, m := range models {
		if m.Name == "Puppy" {
			puppyDecl = m.Declaration
		}
	}
	if puppyDecl == "" {
		t.Fatalf("expected a Puppy declaration, got: %+v", models)
	}
	if !strings.Contains(puppyDecl, "DogName? name;") {
		t.Errorf("expected Puppy.name to inherit Dog's DogName override, got:\n%s", puppyDecl)
	}
	if strings.Contains(puppyDecl, "String name;") {
		t.Errorf("expected Puppy.name to NOT revert to Animal's String type, got:\n%s", puppyDecl)
	}
}

// TestGenerate_AllOfBaseCollapsedToRefAliasStillContributesFields covers
// the "wrap a $ref in allOf just to attach a description" idiom, which
// ir.Build collapses a single-ref, no-own-properties allOf down to a
// bare KindRef alias. Before the fix, flattenExtendsTarget bailed out
// with nil the moment an Extends target's Kind was KindRef instead of
// KindObject, silently dropping every field the alias's underlying
// object declares — with no comment, unlike every other unsupported-shape
// case.
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
	if !strings.Contains(dogDecl, "String id;") {
		t.Errorf("expected Dog to inherit 'id' through the Base alias, got:\n%s", dogDecl)
	}
	if !strings.Contains(dogDecl, "String name;") {
		t.Errorf("expected Dog to still have its own 'name' field, got:\n%s", dogDecl)
	}
}

// TestGenerate_RefToSkippedModelIsOmittedWithComment covers a field that
// $refs a model which itself renders as a comment-only unsupported-shape
// declaration (here a bare oneOf). Before the fix, resolveType's KindRef
// case never checked what the target actually rendered as, so the field
// referenced a Dart type that was never declared — dart analyze:
// undefined class.
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
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
	}
}

// TestGenerate_TypedefToSkippedModelIsCommentOnly covers the
// declaration-level twin of the field-level bug above: a named model
// that is itself a bare $ref to an unsupported-shape model must also be
// omitted with a comment, not rendered as `typedef X = Y;` when Y was
// never declared.
func TestGenerate_TypedefToSkippedModelIsCommentOnly(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Alias":          {Ref: "#/components/schemas/StringOrNumber"},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
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
	if strings.Contains(aliasDecl, "typedef") {
		t.Errorf("expected Alias to be comment-only, not a typedef to an undeclared type, got:\n%s", aliasDecl)
	}
	if !strings.Contains(aliasDecl, "// Alias was not generated for the Dart target: unsupported schema shape") {
		t.Errorf("expected a skip comment, got:\n%s", aliasDecl)
	}
}

// TestGenerate_RefToArrayAliasOfAliasedItemsSerializesAsPrimitiveElements
// pins Critical 1 from task-6's review: effectiveRefNode resolved a $ref
// straight to a top-level array alias, but substituted that array's Items
// node in verbatim, without resolving IT through the same alias logic. So
// an array alias whose items are themselves a $ref to a further alias
// (ItemIds's items are `$ref: ItemId`, and ItemId is itself just
// `{type: string}`) fell through item(To|From)JsonExpr's default $ref
// handling and generated `e.toJson()` / `ItemId.fromJson(...)` — calls
// that don't exist on a String. resolveRefDispatchNode now recurses into
// a resolved array's own Items node (sharing one visited set with the
// outer resolution, so a cyclic alias graph reached this way still
// terminates), fixing this specific case; mirrors
// internal/gen/kotlin's identically-shaped regression test.
func TestGenerate_RefToArrayAliasOfAliasedItemsSerializesAsPrimitiveElements(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"ItemId":  {Type: "string"},
			"ItemIds": {Type: "array", Items: &spec.RawSchema{Ref: "#/components/schemas/ItemId"}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"ids": {Ref: "#/components/schemas/ItemIds"},
				},
				Required: []string{"ids"},
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
	if !strings.Contains(petDecl, "final ItemIds ids;") {
		t.Errorf("expected Pet.ids to keep the ItemIds alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `"ids": ids.map((e) => e).toList(),`) {
		t.Errorf("expected toJson to treat each element as a plain value, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "e.toJson()") {
		t.Errorf("expected elements NOT to be serialized via a nonexistent String.toJson(), got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `ids: (json["ids"] as List<dynamic>).map((e) => e as String).toList(),`) {
		t.Errorf("expected fromJson to cast each element to String directly, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "ItemId.fromJson") {
		t.Errorf("expected elements NOT to be deserialized via a nonexistent ItemId.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_EmptyObjectRendersParameterlessConstructor pins Critical 2:
// Dart's grammar requires a named-parameter list inside `{}` to have at
// least one parameter — `Empty({\n});` does not parse — so a model with
// zero fields must fall back to a plain parameterless constructor.
func TestGenerate_EmptyObjectRendersParameterlessConstructor(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Empty": {Type: "object"},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "Empty({") {
		t.Errorf("expected no empty named-parameter constructor, got:\n%s", decl)
	}
	if !strings.Contains(decl, "class Empty {") {
		t.Errorf("expected a plain Empty class, got:\n%s", decl)
	}
	if !strings.Contains(decl, "Empty();") {
		t.Errorf("expected a parameterless Empty() constructor, got:\n%s", decl)
	}
	if !strings.Contains(decl, "return Empty();") {
		t.Errorf("expected fromJson to return a parameterless Empty(), got:\n%s", decl)
	}
	if !strings.Contains(decl, "return {};") {
		t.Errorf("expected toJson() to return an empty map literal, got:\n%s", decl)
	}
}

// TestGenerate_ObjectWithOnlySkippedFieldRendersParameterlessConstructor
// covers the same Critical 2 gap reached a different way: every property
// is present but gets skipped as an unsupported shape, so the object ends
// up with zero renderable fields even though it started with one.
func TestGenerate_ObjectWithOnlySkippedFieldRendersParameterlessConstructor(t *testing.T) {
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
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "Weird({") {
		t.Errorf("expected no empty named-parameter constructor when every field was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "// value: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"value\" was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "Weird();") {
		t.Errorf("expected a parameterless Weird() constructor, got:\n%s", decl)
	}
}

// TestGenerate_ScalarAliasFieldDependencyIncludesAliasItself pins Critical
// 3(a): a field's declared Dart type always comes from its ORIGINAL
// (pre-alias-resolution) type node, not the alias-resolved effective node
// toJson/fromJson dispatch on. For a scalar alias (PetId = String), the
// effective node is a bare primitive and contributes no dependency at
// all — but the field is still declared `final PetId id;`, so PetId's own
// declaration (and, in modular output, its own file) must still be listed
// as a dependency.
func TestGenerate_ScalarAliasFieldDependencyIncludesAliasItself(t *testing.T) {
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
	var pet dart.ModelOutput
	for _, m := range models {
		if m.Name == "Pet" {
			pet = m
		}
	}
	if len(pet.Dependencies) != 1 || pet.Dependencies[0] != "PetId" {
		t.Fatalf("expected Pet.Dependencies == [\"PetId\"], got %v", pet.Dependencies)
	}
}

// TestGenerate_ArrayAliasFieldDependencyIncludesAliasAndElementType pins
// Critical 3(a)'s other required case: a field typed as a named array
// alias (Categories = List<Category>) needs a dependency on BOTH
// Categories itself (the field's declared type) AND Category (referenced
// inside the generated toJson/fromJson bodies) — not just Category alone,
// which is all the alias-resolved effective node alone would ever surface.
func TestGenerate_ArrayAliasFieldDependencyIncludesAliasAndElementType(t *testing.T) {
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
	var pet dart.ModelOutput
	for _, m := range models {
		if m.Name == "Pet" {
			pet = m
		}
	}
	want := []string{"Categories", "Category"}
	if len(pet.Dependencies) != len(want) {
		t.Fatalf("expected Pet.Dependencies == %v, got %v", want, pet.Dependencies)
	}
	for i, w := range want {
		if pet.Dependencies[i] != w {
			t.Fatalf("expected Pet.Dependencies == %v, got %v", want, pet.Dependencies)
		}
	}
}

// TestGenerate_HyphenatedSchemaNameDependencyIsResolved pins Critical
// 3(b): refNamesIn/collectRefDeps returned the raw IR RefName straight off
// the node, not routed through r.nameFor — but pkg/engine's
// fileNameByModel (for modular output) is keyed by the resolved Dart
// identifier. A hyphenated schema name needs SanitizeTypeIdentifier to
// turn "Pet-Category" into "Pet_Category" before it can key into that map;
// without routing through r.nameFor, the raw name never matches and
// modular output would emit a broken `import ”;`.
func TestGenerate_HyphenatedSchemaNameDependencyIsResolved(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet-Category": {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"Pet": {
				Type: "object",
				Properties: map[string]*spec.RawSchema{
					"category": {Ref: "#/components/schemas/Pet-Category"},
				},
				Required: []string{"category"},
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
	if len(pet.Dependencies) != 1 || pet.Dependencies[0] != "Pet_Category" {
		t.Fatalf("expected Pet.Dependencies == [\"Pet_Category\"] (sanitized, not the raw \"Pet-Category\"), got %v", pet.Dependencies)
	}
}

// TestGenerate_HyphenatedSchemaNameArrayTypedefDependencyIsResolved pins
// Critical 3(b)'s other call site: a top-level named array typedef whose
// items $ref a hyphenated schema name must also resolve that dependency
// through r.nameFor (collectRefDeps, used by renderDeclaration's
// ir.KindArray case), not return the raw schema name.
func TestGenerate_HyphenatedSchemaNameArrayTypedefDependencyIsResolved(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet-Category":  {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
			"PetCategories": {Type: "array", Items: &spec.RawSchema{Ref: "#/components/schemas/Pet-Category"}},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var categories dart.ModelOutput
	for _, m := range models {
		if m.Name == "PetCategories" {
			categories = m
		}
	}
	if len(categories.Dependencies) != 1 || categories.Dependencies[0] != "Pet_Category" {
		t.Fatalf("expected PetCategories.Dependencies == [\"Pet_Category\"] (sanitized, not the raw \"Pet-Category\"), got %v", categories.Dependencies)
	}
}

// TestGenerate_EnumValueCollidingWithEnumMemberNamesIsDisambiguated pins
// Important 4: a JSON enum value literally "value" or "index" would
// otherwise produce a case name colliding with the `final String value;`
// field every generated enum carries, or with the `index` getter Dart's
// built-in Enum base class contributes to every enum automatically —
// neither of which is a Dart language keyword, so plain reservedWords
// handling doesn't catch them.
func TestGenerate_EnumValueCollidingWithEnumMemberNamesIsDisambiguated(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Flag": {Type: "string", Enum: []interface{}{"value", "index"}},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, `value_("value")`) {
		t.Errorf("expected the \"value\" case to be disambiguated to value_, got:\n%s", decl)
	}
	if strings.Contains(decl, `value("value")`) {
		t.Errorf("expected no bare \"value\" case colliding with the enum's own value field, got:\n%s", decl)
	}
	if !strings.Contains(decl, `index_("index")`) {
		t.Errorf("expected the \"index\" case to be disambiguated to index_, got:\n%s", decl)
	}
	if strings.Contains(decl, `index("index")`) {
		t.Errorf("expected no bare \"index\" case colliding with Dart's built-in Enum.index, got:\n%s", decl)
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

// TestGenerate_EmptyStringEnumValueIsNotBareUnderscore pins the
// final-review Critical 1 finding. An `enum: ["", "asc", "desc"]` (a real
// "no sort direction specified" pattern) sanitized its empty value down to
// a bare "_", which modern Dart treats as a wildcard rather than an
// ordinary identifier. Reserving "_" and letting the empty-string case
// fall through the same reserved-word check as every other input turns it
// into "__"; the constructor argument must stay the empty string either
// way.
func TestGenerate_EmptyStringEnumValueIsNotBareUnderscore(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Sort": {Type: "string", Enum: []interface{}{"", "asc", "desc"}},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "    _(") {
		t.Errorf("expected the empty enum value to not become a bare `_`, got:\n%s", decl)
	}
	if !strings.Contains(decl, `__("")`) {
		t.Errorf("expected `__(\"\")`, got:\n%s", decl)
	}
}

// TestGenerate_PunctuationOnlyEnumValuesAreNotBareUnderscore covers the
// other route into Critical 1: a comparison-operator enum, where each
// value's only character is stripped to "_" by invalidIdentChar rather
// than the input being empty to begin with. All four converge on "_", so
// this exercises Critical 1 and Critical 2's enum half together.
func TestGenerate_PunctuationOnlyEnumValuesAreNotBareUnderscore(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Comparison": {Type: "string", Enum: []interface{}{"<", ">", "<=", ">="}},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "    _(") {
		t.Errorf("expected no bare `_` enum value, got:\n%s", decl)
	}
	for _, want := range []string{`__("<")`, `___2(">")`, `___3("<=")`, `___4(">=")`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
}

// TestGenerate_PropertyNameSanitizingToLeadingUnderscoreIsNotPrivate
// covers a JSON property name whose punctuation all gets rewritten to
// "_" by invalidIdentChar, landing on a leading underscore — real-world
// examples include Microsoft Graph's "@odata.type", JSON-LD's "@id"/
// "@type", and "$schema". A leading underscore makes a Dart identifier
// library-private: illegal outright as a named constructor parameter
// ("Named parameters can't start with an underscore" — a hard compile
// error) and, on a class, unusable from any other file in modular
// output. "$" is itself a legal identifier-start character in Dart that
// carries no privacy meaning, so sanitize prepends it whenever the
// result would otherwise start with "_".
func TestGenerate_PropertyNameSanitizingToLeadingUnderscoreIsNotPrivate(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Pet": {
				Type:       "object",
				Properties: map[string]*spec.RawSchema{"@odata.type": {Type: "string"}},
				Required:   []string{"@odata.type"},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "this._") || strings.Contains(decl, "final String _") {
		t.Errorf("expected no leading-underscore (private) field or named parameter, got:\n%s", decl)
	}
	if !strings.Contains(decl, "final String $_odata_type;") {
		t.Errorf("expected the sanitized property to be prefixed with $ instead of left private, got:\n%s", decl)
	}
}

// TestGenerate_CollidingFieldNamesAreUniquified pins the final-review
// Critical 2 finding: two DIFFERENT wire names converging on one camelCase
// identifier used to emit two identically-named fields (`final String
// photoUrls;` twice). Each field must keep its OWN wire name in
// toJson()/fromJson().
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
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Count(decl, "final String photoUrls;") != 1 {
		t.Errorf("expected exactly one `final String photoUrls;`, got:\n%s", decl)
	}
	if !strings.Contains(decl, "final String photoUrls_2;") {
		t.Errorf("expected the colliding second field to be uniquified, got:\n%s", decl)
	}
	for _, want := range []string{
		`"photoUrls": photoUrls,`,
		`"photo_urls": photoUrls_2,`,
		`photoUrls: json["photoUrls"] as String,`,
		`photoUrls_2: json["photo_urls"] as String,`,
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q — each field must round-trip through its own wire name — in:\n%s", want, decl)
		}
	}
}

// TestGenerate_CollidingEnumValueNamesAreUniquified is Critical 2's enum
// half: two different wire values converging on one lowerCamelCase enum
// value identifier.
func TestGenerate_CollidingEnumValueNamesAreUniquified(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Kind": {Type: "string", Enum: []interface{}{"photo_urls", "photoUrls"}},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	for _, want := range []string{`photoUrls("photo_urls")`, `photoUrls_2("photoUrls")`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
}

// TestGenerate_TopLevelArrayItemUsesItemSuffix pins the final-review
// Important 1 finding: Swift's renderDeclaration passes name+"Item" as the
// suggested name for a top-level array's item type, and the design spec
// requires the same synthesized-name convention in all three languages.
// Dart passed bare `name`, so the item type landed on "Things_2" (saved
// from a literal self-reference only by uniquify's numeric fallback).
func TestGenerate_TopLevelArrayItemUsesItemSuffix(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Things": {
				Type: "array",
				Items: &spec.RawSchema{
					Type:       "object",
					Properties: map[string]*spec.RawSchema{"id": {Type: "string"}},
					Required:   []string{"id"},
				},
			},
		},
	}
	models, err := dart.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var alias string
	var haveItem bool
	for _, m := range models {
		switch m.Name {
		case "Things":
			alias = m.Declaration
		case "ThingsItem":
			haveItem = true
		}
	}
	if !haveItem {
		t.Fatalf("expected a synthesized ThingsItem declaration, got: %+v", models)
	}
	if !strings.Contains(alias, "typedef Things = List<ThingsItem>;") {
		t.Errorf("expected Things to alias List<ThingsItem>, got:\n%s", alias)
	}
}
