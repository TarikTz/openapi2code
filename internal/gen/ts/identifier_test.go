package ts_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
)

func TestSanitizeIdentifier(t *testing.T) {
	cases := map[string]string{
		"Pet":        "Pet",
		"Pet-Status": "Pet_Status",
		"2Pets":      "_2Pets",
		"":           "_",
	}
	for input, want := range cases {
		got := ts.SanitizeIdentifier(input)
		if got != want {
			t.Errorf("SanitizeIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeIdentifier_ReservedWords(t *testing.T) {
	cases := map[string]string{
		"class":     "class_",
		"enum":      "enum_",
		"interface": "interface_",
		"type":      "type_",
		"function":  "function_",
		"default":   "default_",
		"null":      "null_",
		"Class":     "Class",
		"classy":    "classy",
	}
	for input, want := range cases {
		got := ts.SanitizeIdentifier(input)
		if got != want {
			t.Errorf("SanitizeIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}
