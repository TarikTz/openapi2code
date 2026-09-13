// Package swift generates Swift Codable structs/classes from the IR.
package swift

import (
	"regexp"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Swift keywords that cannot be used as a bare
// identifier (a subset commonly hit by real-world OpenAPI schema/field
// names; Swift technically allows any keyword as an identifier via
// backticks, but suffixing is simpler and matches this project's existing
// TS/Zod precedent).
var reservedWords = map[string]bool{
	"associatedtype": true, "class": true, "deinit": true, "enum": true,
	"extension": true, "fileprivate": true, "func": true, "import": true,
	"init": true, "inout": true, "internal": true, "let": true,
	"open": true, "operator": true, "private": true, "protocol": true,
	"public": true, "rethrows": true, "static": true, "struct": true,
	"subscript": true, "typealias": true, "var": true, "break": true,
	"case": true, "continue": true, "default": true, "defer": true,
	"do": true, "else": true, "fallthrough": true, "for": true,
	"guard": true, "if": true, "in": true, "repeat": true, "return": true,
	"switch": true, "where": true, "while": true, "as": true, "Any": true,
	"catch": true, "false": true, "is": true, "nil": true, "self": true,
	"Self": true, "super": true, "throw": true, "throws": true,
	"true": true, "try": true,
	// "_" is Swift's wildcard pattern, not a usable identifier ("keyword
	// '_' cannot be used as an identifier here"). It is reserved here so
	// that the empty/punctuation-only inputs sanitize() folds down to "_"
	// (an `enum: ["", "asc"]` value, a `<` comparison-operator enum value)
	// pick up the same trailing-underscore disambiguation every other
	// keyword gets, becoming "__".
	"_": true,
}

// sanitize maps an arbitrary JSON name onto a valid Swift identifier.
// The empty-string case assigns "_" rather than returning it, so it flows
// through the SAME digit-prefix and reserved-word checks as every other
// input — "_" is itself reserved (see reservedWords), so an empty name
// ends up as "__", not as the non-compiling bare "_".
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
// valid Swift type identifier. Casing is left as-is: OpenAPI schema names
// and this package's own synthesized names (see resolveType) are already
// PascalCase.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Swift property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// SanitizeEnumCaseIdentifier converts a JSON enum value into a valid,
// idiomatic Swift enum case identifier.
func SanitizeEnumCaseIdentifier(value string) string {
	return sanitize(mobile.ToCamelCase(value))
}
