package spec

import "testing"

func TestResolveRefName_V3(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	name, err := doc.ResolveRefName("#/components/schemas/Pet")
	if err != nil {
		t.Fatalf("ResolveRefName: %v", err)
	}
	if name != "Pet" {
		t.Errorf("got %q, want Pet", name)
	}
}

func TestResolveRefName_V2(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	name, err := doc.ResolveRefName("#/definitions/Pet")
	if err != nil {
		t.Fatalf("ResolveRefName: %v", err)
	}
	if name != "Pet" {
		t.Errorf("got %q, want Pet", name)
	}
}

func TestResolveRefName_Unresolvable(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{"Pet": {Type: "object"}}}
	if _, err := doc.ResolveRefName("#/components/schemas/Missing"); err == nil {
		t.Fatal("expected error for unresolvable ref, got nil")
	}
}

func TestResolveRefName_External(t *testing.T) {
	doc := &RawDocument{Schemas: map[string]*RawSchema{}}
	if _, err := doc.ResolveRefName("other.yaml#/Pet"); err == nil {
		t.Fatal("expected error for external ref, got nil")
	}
}

func TestParse_ValidatesRefs(t *testing.T) {
	_, err := Parse([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Pet": {
					"type": "object",
					"properties": {
						"category": {"$ref": "#/components/schemas/Missing"}
					}
				}
			}
		}
	}`))
	if err == nil {
		t.Fatal("expected error for unresolvable ref, got nil")
	}
}

func TestParse_Success(t *testing.T) {
	doc, err := Parse([]byte(`{
		"openapi": "3.0.3",
		"components": {
			"schemas": {
				"Category": {"type": "object"},
				"Pet": {
					"type": "object",
					"properties": {
						"category": {"$ref": "#/components/schemas/Category"}
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(doc.Schemas))
	}
}
