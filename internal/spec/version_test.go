package spec

import (
	"strings"
	"testing"
)

func TestNormalizeToJSON_PassesThroughJSON(t *testing.T) {
	input := []byte(`{"openapi":"3.0.3"}`)
	got, err := normalizeToJSON(input)
	if err != nil {
		t.Fatalf("normalizeToJSON: %v", err)
	}
	if string(got) != string(input) {
		t.Errorf("got %s, want %s", got, input)
	}
}

func TestNormalizeToJSON_ConvertsYAML(t *testing.T) {
	input := []byte("openapi: 3.0.3\ninfo:\n  title: Test\n")
	got, err := normalizeToJSON(input)
	if err != nil {
		t.Fatalf("normalizeToJSON: %v", err)
	}
	version, err := DetectVersion(got)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got version %v, want VersionV3", version)
	}
}

func TestNormalizeToJSON_NonStringMappingKeys(t *testing.T) {
	// Unquoted numeric HTTP status codes under an ignored top-level section
	// make yaml.Unmarshal produce map[interface{}]interface{}, which
	// json.Marshal cannot encode unless the keys are normalized first.
	input := []byte("openapi: 3.0.3\n" +
		"paths:\n" +
		"  /pets:\n" +
		"    get:\n" +
		"      responses:\n" +
		"        200:\n" +
		"          description: ok\n" +
		"        404:\n" +
		"          description: missing\n" +
		"components:\n" +
		"  schemas:\n" +
		"    Pet:\n" +
		"      type: object\n")
	got, err := normalizeToJSON(input)
	if err != nil {
		t.Fatalf("normalizeToJSON: %v", err)
	}
	version, err := DetectVersion(got)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got version %v, want VersionV3", version)
	}
	if !strings.Contains(string(got), `"200"`) {
		t.Errorf("expected numeric key to be stringified, got %s", got)
	}
}

func TestParse_YAMLWithNonStringMappingKeys(t *testing.T) {
	doc, err := Parse([]byte("openapi: 3.0.3\n" +
		"paths:\n" +
		"  /pets:\n" +
		"    get:\n" +
		"      responses:\n" +
		"        200:\n" +
		"          description: ok\n" +
		"components:\n" +
		"  schemas:\n" +
		"    Pet:\n" +
		"      type: object\n" +
		"      properties:\n" +
		"        name:\n" +
		"          type: string\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := doc.Schemas["Pet"]; !ok {
		t.Fatalf("expected schema Pet, got %v", doc.Schemas)
	}
}

func TestDetectVersion_V3(t *testing.T) {
	version, err := DetectVersion([]byte(`{"openapi":"3.0.3"}`))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got %v, want VersionV3", version)
	}
}

func TestDetectVersion_V2(t *testing.T) {
	version, err := DetectVersion([]byte(`{"swagger":"2.0"}`))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV2 {
		t.Errorf("got %v, want VersionV2", version)
	}
}

func TestDetectVersion_V31(t *testing.T) {
	version, err := DetectVersion([]byte(`{"openapi":"3.1.0"}`))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != VersionV3 {
		t.Errorf("got %v, want VersionV3", version)
	}
}

func TestDetectVersion_V32Rejected(t *testing.T) {
	_, err := DetectVersion([]byte(`{"openapi":"3.2.0"}`))
	if err == nil {
		t.Fatal("expected error for OpenAPI 3.2, got nil")
	}
	if !strings.Contains(err.Error(), "3.2") {
		t.Errorf("expected an error naming 3.2 specifically, got %q", err)
	}
	if strings.Contains(err.Error(), "missing or unrecognized") {
		t.Errorf("expected a 3.2-specific error, got the generic one: %q", err)
	}
}

func TestDetectVersion_Unknown(t *testing.T) {
	_, err := DetectVersion([]byte(`{"foo":"bar"}`))
	if err == nil {
		t.Fatal("expected error for unrecognized document, got nil")
	}
}
