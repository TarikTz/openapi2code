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

func TestAdditionalProperties_SchemaForm(t *testing.T) {
	doc, err := ParseRaw([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Metadata": {
					"type": "object",
					"additionalProperties": {"type": "string"}
				}
			}
		}
	}`), VersionV3)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	ap := doc.Schemas["Metadata"].AdditionalProperties
	if ap == nil {
		t.Fatal("expected AdditionalProperties to be set")
	}
	if ap.Schema == nil || ap.Schema.Type != "string" {
		t.Errorf("expected a string value schema, got %+v", ap)
	}
	if !ap.Allowed {
		t.Error("expected Allowed to be true when a schema is given")
	}
}

func TestAdditionalProperties_BooleanTrueForm(t *testing.T) {
	doc, err := ParseRaw([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Freeform": {"type": "object", "additionalProperties": true}
			}
		}
	}`), VersionV3)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	ap := doc.Schemas["Freeform"].AdditionalProperties
	if ap == nil {
		t.Fatal("expected AdditionalProperties to be set")
	}
	if ap.Schema != nil {
		t.Errorf("expected no schema for the bare boolean form, got %+v", ap.Schema)
	}
	if !ap.Allowed {
		t.Error("expected Allowed to be true")
	}
}

func TestAdditionalProperties_BooleanFalseForm(t *testing.T) {
	doc, err := ParseRaw([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Closed": {"type": "object", "additionalProperties": false}
			}
		}
	}`), VersionV3)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	ap := doc.Schemas["Closed"].AdditionalProperties
	if ap == nil {
		t.Fatal("expected AdditionalProperties to be set")
	}
	if ap.Allowed {
		t.Error("expected Allowed to be false")
	}
	if ap.Schema != nil {
		t.Errorf("expected no schema, got %+v", ap.Schema)
	}
}

func TestAdditionalProperties_RefInsideSchemaFormIsValidated(t *testing.T) {
	_, err := Parse([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Metadata": {
					"type": "object",
					"additionalProperties": {"$ref": "#/components/schemas/DoesNotExist"}
				}
			}
		}
	}`))
	if err == nil {
		t.Fatal("expected an error for a dangling $ref inside additionalProperties")
	}
}
