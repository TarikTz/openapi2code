// Package kotlin generates Kotlin data classes from the IR.
package kotlin

import (
	"regexp"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Kotlin hard keywords that cannot be used as a bare
// identifier.
var reservedWords = map[string]bool{
	"as": true, "break": true, "class": true, "continue": true,
	"do": true, "else": true, "false": true, "for": true, "fun": true,
	"if": true, "in": true, "interface": true, "is": true, "null": true,
	"object": true, "package": true, "return": true, "super": true,
	"this": true, "throw": true, "true": true, "try": true, "typealias": true,
	"typeof": true, "val": true, "var": true, "when": true, "while": true,
	// "_" is reserved in Kotlin (it is the unused-parameter placeholder and
	// is explicitly not a usable identifier). Reserving it here also means
	// the empty/punctuation-only inputs sanitize() folds down to "_" (an
	// `enum: ["", "asc"]` value, a `<` comparison-operator enum value) pick
	// up the same trailing-underscore disambiguation every other keyword
	// gets, becoming "__".
	"_": true,
}

// sanitize maps an arbitrary JSON name onto a valid Kotlin identifier.
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
// valid Kotlin type identifier. Casing is left as-is.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Kotlin property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// SanitizeEnumConstantIdentifier converts a JSON enum value into a valid,
// idiomatic Kotlin enum constant identifier (SCREAMING_SNAKE_CASE by
// community convention).
func SanitizeEnumConstantIdentifier(value string) string {
	return sanitize(mobile.ToScreamingSnakeCase(value))
}
