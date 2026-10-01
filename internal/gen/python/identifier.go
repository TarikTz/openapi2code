// Package python generates Python dataclass and Pydantic models from the
// IR, selected via Style (see generate.go).
package python

import (
	"regexp"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Python's keywords (keyword.kwlist), which cannot be
// used as a bare identifier.
var reservedWords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true,
	"assert": true, "async": true, "await": true, "break": true,
	"class": true, "continue": true, "def": true, "del": true,
	"elif": true, "else": true, "except": true, "finally": true,
	"for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true,
	"nonlocal": true, "not": true, "or": true, "pass": true,
	"raise": true, "return": true, "try": true, "while": true,
	"with": true, "yield": true,
}

// sanitize maps an arbitrary JSON name onto a valid Python identifier.
// Mirrors internal/gen/swift's identifier.go sanitize exactly.
func sanitize(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		sanitized = "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	if reservedWords[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// SanitizeTypeIdentifier converts a model or synthesized type name into a
// valid Python class identifier. Casing is left as-is: OpenAPI schema
// names and this package's own synthesized names (see resolveType) are
// already PascalCase.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Python (snake_case) attribute identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToSnakeCase(name))
}

// SanitizeEnumMemberIdentifier converts a JSON enum value into a valid,
// idiomatic Python (SCREAMING_SNAKE_CASE) enum member identifier.
func SanitizeEnumMemberIdentifier(value string) string {
	return sanitize(mobile.ToScreamingSnakeCase(value))
}
