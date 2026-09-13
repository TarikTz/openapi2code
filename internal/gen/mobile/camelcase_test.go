package mobile_test

import (
	"testing"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

func TestToCamelCase(t *testing.T) {
	cases := map[string]string{
		"photo_urls":   "photoUrls",
		"photoUrls":    "photoUrls",
		"PhotoUrls":    "photoUrls",
		"name":         "name",
		"in-progress":  "inProgress",
		"already done": "alreadyDone",
		"":             "",
	}
	for input, want := range cases {
		if got := mobile.ToCamelCase(input); got != want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	cases := map[string]string{
		"status":     "Status",
		"photo_urls": "PhotoUrls",
		"":           "",
	}
	for input, want := range cases {
		if got := mobile.ToPascalCase(input); got != want {
			t.Errorf("ToPascalCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToScreamingSnakeCase(t *testing.T) {
	cases := map[string]string{
		"available":   "AVAILABLE",
		"in_progress": "IN_PROGRESS",
		"inProgress":  "IN_PROGRESS",
		"in-progress": "IN_PROGRESS",
	}
	for input, want := range cases {
		if got := mobile.ToScreamingSnakeCase(input); got != want {
			t.Errorf("ToScreamingSnakeCase(%q) = %q, want %q", input, got, want)
		}
	}
}
