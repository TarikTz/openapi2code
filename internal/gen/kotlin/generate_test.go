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
	decl := models[0].Declaration
	if strings.Contains(decl, "val weird") {
		t.Errorf("expected the union-typed field to be omitted as a real property, got:\n%s", decl)
	}
	if !strings.Contains(decl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"weird\" was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "val name: String") {
		t.Errorf("expected the model's other field to still be generated, got:\n%s", decl)
	}
}

// TestGenerate_RefToArrayOfInlineObjectFieldIsSkipped guards a field
// whose $ref points at a top-level array alias with an INLINE (non-$ref)
// object item — e.g. `Things: {type: array, items: {type: object, ...}}`.
// Before this fix, such a field still declared its type as the alias
// (`val things: Things`) but generated `things.map { it }`/
// `(json["things"] as List<*>).map { it }` for its (de)serialization:
// `.map { it }` infers List<Any?>, which does not satisfy the
// List<ThingsItem>-typed constructor parameter — a Kotlin compile error,
// not merely wrong output. The alias itself (Things) still renders
// correctly; only a field referencing it through this exact shape is
// unrepresentable and must be skipped with a comment instead.
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
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	if strings.Contains(containerDecl, "val things") {
		t.Errorf("expected the things field to be omitted as a real property, got:\n%s", containerDecl)
	}
	if !strings.Contains(containerDecl, "// things: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"things\" was skipped, got:\n%s", containerDecl)
	}
	if strings.Contains(containerDecl, ".map { it }") {
		t.Errorf("must not generate the broken .map { it } dispatch (infers List<Any?>, a compile error), got:\n%s", containerDecl)
	}
	if !strings.Contains(thingsDecl, "typealias Things = List<ThingsItem>") {
		t.Errorf("expected Things itself to still render correctly, got:\n%s", thingsDecl)
	}
}

// TestGenerate_UnionModelIsSkippedEntirely pins Important Finding 6's
// model-level half: a top-level schema with no clean Kotlin representation
// (here, a bare oneOf) must still appear in Generate's output as a
// comment-only declaration explaining why, not be silently dropped, while
// generation continues normally for the rest of the document.
func TestGenerate_UnionModelIsSkippedEntirely(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Pet":            {Type: "object", Properties: map[string]*spec.RawSchema{"name": {Type: "string"}}, Required: []string{"name"}},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	if !strings.Contains(stringOrNumberDecl, "// StringOrNumber was not generated for the Kotlin target: unsupported schema shape") {
		t.Errorf("expected a comment explaining why StringOrNumber was skipped, got:\n%s", stringOrNumberDecl)
	}
	if strings.Contains(stringOrNumberDecl, "class") || strings.Contains(stringOrNumberDecl, "enum") {
		t.Errorf("expected StringOrNumber's declaration to be comment-only, got:\n%s", stringOrNumberDecl)
	}
}

// TestGenerate_EmptyObjectIsPlainClassNotDataClass pins Critical Finding 1:
// a data class with zero primary-constructor parameters
// ("data class Empty()") does not compile in Kotlin ("Data class must have
// at least one primary constructor parameter"). A bare `{type: object}`
// schema with no properties at all must fall back to a plain class with a
// parameterless constructor instead.
func TestGenerate_EmptyObjectIsPlainClassNotDataClass(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Empty": {Type: "object"},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "data class") {
		t.Errorf("expected a plain class (not a data class) for a model with zero fields, got:\n%s", decl)
	}
	if !strings.Contains(decl, "class Empty {") {
		t.Errorf("expected a plain Empty class, got:\n%s", decl)
	}
	if !strings.Contains(decl, "return emptyMap()") {
		t.Errorf("expected toJson() to return emptyMap(), got:\n%s", decl)
	}
	if !strings.Contains(decl, "return Empty()") {
		t.Errorf("expected fromJson to return a parameterless Empty(), got:\n%s", decl)
	}
}

// TestGenerate_ObjectWithOnlySkippedFieldIsPlainClass covers the same
// Critical Finding 1 gap reached a different way: every property is
// present but gets skipped as an unsupported shape, so the object ends up
// with zero renderable fields even though it started with one.
func TestGenerate_ObjectWithOnlySkippedFieldIsPlainClass(t *testing.T) {
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "data class") {
		t.Errorf("expected a plain class when every field was skipped, got:\n%s", decl)
	}
	if !strings.Contains(decl, "// value: skipped — unsupported schema shape") {
		t.Errorf("expected a comment explaining why \"value\" was skipped, got:\n%s", decl)
	}
}

// TestGenerate_RefKindLookupUsesResolvedNameNotRawSchemaName pins Critical
// Finding 2: r.kinds is keyed by the resolved Kotlin identifier (see
// Generate), but a $ref's RefName carries the raw IR schema name — these
// only agree when the schema name needs no sanitization. A schema named
// with a hyphen (a common real-world pattern) needs SanitizeTypeIdentifier
// to turn "Pet-Status" into "Pet_Status" before it can key into r.kinds;
// without routing through r.nameFor first, the lookup silently misses and
// falls back to object-shaped dispatch (.toJson()/.fromJson()) for what is
// actually an enum.
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
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "status.value") {
		t.Errorf("expected toJson to serialize status via .value (enum dispatch), got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "status.toJson()") {
		t.Errorf("expected status to NOT be dispatched as an object, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "Pet_Status.fromValue(json[\"status\"] as String)") {
		t.Errorf("expected fromJson to deserialize status via Pet_Status.fromValue, got:\n%s", petDecl)
	}
}

// TestGenerate_RefToTopLevelScalarAliasSerializesAsPrimitive pins Critical
// Finding 3: a $ref to a top-level named scalar (e.g. `PetId: {type:
// string}`, generated as `typealias PetId = String`) must serialize as a
// plain String, not as an object with a nonexistent PetId.toJson()/
// PetId.fromJson() — a Kotlin typealias carries no such methods of its
// own, since it is not a distinct wrapper type.
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
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "val id: PetId") {
		t.Errorf("expected Pet.id to keep the PetId alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `map["id"] = id`) {
		t.Errorf("expected toJson to serialize id as a plain value, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "id.toJson()") || strings.Contains(petDecl, "id?.toJson()") {
		t.Errorf("expected id NOT to be serialized via a nonexistent PetId.toJson(), got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `id = json["id"] as String`) {
		t.Errorf("expected fromJson to deserialize id as a plain String cast, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "PetId.fromJson") {
		t.Errorf("expected id NOT to be deserialized via a nonexistent PetId.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_RefToTopLevelArrayAliasSerializesElementwise pins Critical
// Finding 3's other required case: a $ref to a top-level named array
// (`Categories: {type: array, items: {$ref: Category}}`, generated as
// `typealias Categories = List<Category>`) must serialize element-wise via
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
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "val categories: Categories") {
		t.Errorf("expected Pet.categories to keep the Categories alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `map["categories"] = categories.map { it.toJson() }`) {
		t.Errorf("expected toJson to map categories element-wise via Category.toJson(), got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "categories.toJson()") {
		t.Errorf("expected categories NOT to be serialized as if it were a single object, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `categories = (json["categories"] as List<*>).map { Category.fromJson(it as Map<String, Any?>) }`) {
		t.Errorf("expected fromJson to map categories element-wise via Category.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_RefToArrayAliasOfAliasedItemsSerializesAsPrimitiveElements
// pins the Important gap found in the scoped re-review of Critical Finding
// 3's own fix: effectiveRefNode resolved a $ref straight to a top-level
// array alias, but substituted that array's Items node in verbatim,
// without resolving IT through the same alias logic. So an array alias
// whose items are themselves a $ref to a further alias (ItemIds's items
// are `$ref: ItemId`, and ItemId is itself just `{type: string}`) fell
// through item(To|From)JsonExpr's default $ref handling and generated
// `it.toJson()` / `ItemId.fromJson(...)` — calls that don't exist on a
// String. resolveRefDispatchNode now recurses into a resolved array's own
// Items node (sharing one visited set with the outer resolution, so a
// cyclic alias graph reached this way still terminates), fixing this
// specific case; see that function's doc comment for the sibling case
// (an array alias whose items are an INLINE, non-$ref object) that is
// intentionally NOT fixed here and why.
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
		t.Fatalf("expected a Pet declaration, got: %+v", models)
	}
	if !strings.Contains(petDecl, "val ids: ItemIds") {
		t.Errorf("expected Pet.ids to keep the ItemIds alias as its declared type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `map["ids"] = ids.map { it }`) {
		t.Errorf("expected toJson to treat each element as a plain value, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "it.toJson()") {
		t.Errorf("expected elements NOT to be serialized via a nonexistent String.toJson(), got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `ids = (json["ids"] as List<*>).map { it as String }`) {
		t.Errorf("expected fromJson to cast each element to String directly, got:\n%s", petDecl)
	}
	if strings.Contains(petDecl, "ItemId.fromJson") {
		t.Errorf("expected elements NOT to be deserialized via a nonexistent ItemId.fromJson(...), got:\n%s", petDecl)
	}
}

// TestGenerate_NestedArrayPreservesInnerTransform pins Critical Finding 4:
// an array of arrays (List<List<String>>) must apply the element
// transform at every nesting level, not just the outer one — the inner
// .map{} was previously a bare "it" (fromJson) or entirely missing
// (toJson), producing a value statically typed List<Any?>, which does not
// match a List<List<String>> constructor parameter.
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "val rows: List<List<String>>") {
		t.Errorf("expected rows: List<List<String>>, got:\n%s", decl)
	}
	if !strings.Contains(decl, `map["rows"] = rows.map { it.map { it } }`) {
		t.Errorf("expected toJson to nest the .map{} transform, got:\n%s", decl)
	}
	if !strings.Contains(decl, `rows = (json["rows"] as List<*>).map { (it as List<*>).map { it as String } }`) {
		t.Errorf("expected fromJson to nest the cast-and-map transform, got:\n%s", decl)
	}
}

// TestGenerate_FlattenFields_OwnFieldOverridesBaseField pins Important
// Finding 5: when a derived (allOf) schema redeclares a field its base
// also declares, the derived schema's own field must win — matching
// internal/gen/ts's `extends` (where the subtype's own declaration
// shadows the base's) and internal/gen/zod's `.extend()` (whose
// own-fields object is merged in last), and mirroring
// internal/gen/swift's identical fix for the same gap.
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
		t.Fatalf("expected a Dog declaration, got: %+v", models)
	}
	if !strings.Contains(dogDecl, "name: DogName") {
		t.Errorf("expected Dog.name to use the overriding DogName type, got:\n%s", dogDecl)
	}
	if strings.Contains(dogDecl, "name: String") {
		t.Errorf("expected Dog.name to NOT fall back to the base Animal's String type, got:\n%s", dogDecl)
	}
}

// TestGenerate_MapFieldOfPrimitive covers a field built from
// additionalProperties (ir.KindMap) whose values are a plain primitive —
// the simplest case, needing no per-entry transform beyond the cast.
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if !strings.Contains(decl, "val metadata: Map<String, String>? = null") {
		t.Errorf("expected a Map<String, String>? field, got:\n%s", decl)
	}
	if !strings.Contains(decl, `map["metadata"] = metadata?.mapValues { (_, it) -> it }`) {
		t.Errorf("expected a per-entry toJson passthrough for a primitive-valued map, got:\n%s", decl)
	}
	if !strings.Contains(decl, `metadata = (json["metadata"] as? Map<String, Any?>)?.mapValues { (_, it) -> it as String }`) {
		t.Errorf("expected a per-entry String cast in fromJson, got:\n%s", decl)
	}
}

// TestGenerate_MapFieldOfRefObject covers a map whose values are a $ref
// to an object model — each entry needs .toJson()/.fromJson() called on
// its VALUE, not its key, via the "(_, it)" destructuring that reuses
// itemToJsonExpr/itemFromJsonExpr's existing "it"-based output verbatim.
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
	if !strings.Contains(petDecl, "val tagsByID: Map<String, Tag>") {
		t.Errorf("expected a Map<String, Tag> field, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `tagsByID.mapValues { (_, it) -> it.toJson() }`) {
		t.Errorf("expected toJson() called per-entry on the map's values, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, `(json["tagsByID"] as Map<String, Any?>).mapValues { (_, it) -> Tag.fromJson(it as Map<String, Any?>) }`) {
		t.Errorf("expected Tag.fromJson() called per-entry, got:\n%s", petDecl)
	}
}

// TestGenerate_TopLevelMapOfRef covers a named top-level map schema whose
// values are a $ref, checking the generated typealias.
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var tagsByIDDecl string
	for _, m := range models {
		if m.Name == "TagsByID" {
			tagsByIDDecl = m.Declaration
		}
	}
	if tagsByIDDecl != "typealias TagsByID = Map<String, Tag>\n" {
		t.Errorf("got %q", tagsByIDDecl)
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
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
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
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	if !strings.Contains(puppyDecl, "name: DogName") {
		t.Errorf("expected Puppy.name to inherit Dog's DogName override, got:\n%s", puppyDecl)
	}
	if strings.Contains(puppyDecl, "name: String") {
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
		t.Fatalf("expected a Dog declaration, got: %+v", models)
	}
	if !strings.Contains(dogDecl, "id: String") {
		t.Errorf("expected Dog to inherit 'id' through the Base alias, got:\n%s", dogDecl)
	}
	if !strings.Contains(dogDecl, "name: String") {
		t.Errorf("expected Dog to still have its own 'name' field, got:\n%s", dogDecl)
	}
}

// TestGenerate_RefToSkippedModelIsOmittedWithComment covers a field that
// $refs a model which itself renders as a comment-only unsupported-shape
// declaration (here a bare oneOf). Before the fix, resolveType's KindRef
// case never checked what the target actually rendered as, so the field
// referenced a Kotlin type that was never declared — kotlinc: unresolved
// reference, and since this generator's monolithic output is one file,
// the WHOLE file fails to compile, not just this one model.
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
	if strings.Contains(petDecl, "StringOrNumber") {
		t.Errorf("expected Pet to not reference the undeclared StringOrNumber type, got:\n%s", petDecl)
	}
	if !strings.Contains(petDecl, "// weird: skipped — unsupported schema shape") {
		t.Errorf("expected a skip comment for weird, got:\n%s", petDecl)
	}
}

// TestGenerate_TypealiasToSkippedModelIsCommentOnly covers the
// declaration-level twin of the field-level bug above: a named model
// that is itself a bare $ref to an unsupported-shape model must also be
// omitted with a comment, not rendered as `typealias X = Y` when Y was
// never declared.
func TestGenerate_TypealiasToSkippedModelIsCommentOnly(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"StringOrNumber": {OneOf: []*spec.RawSchema{{Type: "string"}, {Type: "number"}}},
			"Alias":          {Ref: "#/components/schemas/StringOrNumber"},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	if !strings.Contains(aliasDecl, "// Alias was not generated for the Kotlin target: unsupported schema shape") {
		t.Errorf("expected a skip comment, got:\n%s", aliasDecl)
	}
}

// TestGenerate_EmptyStringEnumValueIsNotBareUnderscore pins the
// final-review Critical 1 finding. An `enum: ["", "asc", "desc"]` (a real
// "no sort direction specified" pattern) sanitized its empty value down to
// a bare "_", which is not a usable Kotlin identifier. Reserving "_" and
// letting the empty-string case fall through the same reserved-word check
// as every other input turns it into "__"; the constructor argument must
// stay the empty string either way.
func TestGenerate_EmptyStringEnumValueIsNotBareUnderscore(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Sort": {Type: "string", Enum: []interface{}{"", "asc", "desc"}},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Contains(decl, "    _(") {
		t.Errorf("expected no bare `_` constant, got:\n%s", decl)
	}
	for _, want := range []string{`__("<")`, `___2(">")`, `___3("<=")`, `___4(">=")`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
}

// TestGenerate_CollidingFieldNamesAreUniquified pins the final-review
// Critical 2 finding: two DIFFERENT wire names converging on one camelCase
// identifier used to emit two identically-named constructor parameters
// (`val photoUrls: String, val photoUrls: String`). Each property must
// keep its OWN wire name in toJson()/fromJson().
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
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	if strings.Count(decl, "val photoUrls: String") != 1 {
		t.Errorf("expected exactly one `val photoUrls: String`, got:\n%s", decl)
	}
	if !strings.Contains(decl, "val photoUrls_2: String") {
		t.Errorf("expected the colliding second field to be uniquified, got:\n%s", decl)
	}
	for _, want := range []string{
		`map["photoUrls"] = photoUrls`,
		`map["photo_urls"] = photoUrls_2`,
		`photoUrls = json["photoUrls"] as String`,
		`photoUrls_2 = json["photo_urls"] as String`,
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q — each property must round-trip through its own wire name — in:\n%s", want, decl)
		}
	}
}

// TestGenerate_CollidingEnumValueNamesAreUniquified is Critical 2's enum
// half: two different wire values converging on one SCREAMING_SNAKE_CASE
// constant.
func TestGenerate_CollidingEnumValueNamesAreUniquified(t *testing.T) {
	raw := &spec.RawDocument{
		Schemas: map[string]*spec.RawSchema{
			"Kind": {Type: "string", Enum: []interface{}{"photo_urls", "photoUrls"}},
		},
	}
	models, err := kotlin.Generate(buildDoc(t, raw))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	decl := models[0].Declaration
	for _, want := range []string{`PHOTO_URLS("photo_urls")`, `PHOTO_URLS_2("photoUrls")`} {
		if !strings.Contains(decl, want) {
			t.Errorf("expected %q in:\n%s", want, decl)
		}
	}
}

// TestGenerate_TopLevelArrayItemUsesItemSuffix pins the final-review
// Important 1 finding: Swift's renderDeclaration passes name+"Item" as the
// suggested name for a top-level array's item type, and the design spec
// requires the same synthesized-name convention in all three languages.
// Kotlin passed bare `name`, so the item type landed on "Things_2" (saved
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
	models, err := kotlin.Generate(buildDoc(t, raw))
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
	if !strings.Contains(alias, "typealias Things = List<ThingsItem>") {
		t.Errorf("expected Things to alias List<ThingsItem>, got:\n%s", alias)
	}
}
