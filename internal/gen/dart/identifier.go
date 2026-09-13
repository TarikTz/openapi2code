// Package dart generates Dart classes from the IR.
package dart

import (
	"regexp"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// reservedWords are Dart reserved words that cannot be used as a bare
// identifier.
var reservedWords = map[string]bool{
	"assert": true, "break": true, "case": true, "catch": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "else": true, "enum": true, "extends": true, "false": true,
	"final": true, "finally": true, "for": true, "if": true, "in": true,
	"is": true, "new": true, "null": true, "rethrow": true, "return": true,
	"super": true, "switch": true, "this": true, "throw": true,
	"true": true, "try": true, "var": true, "void": true, "while": true,
	"with": true,
	// "_" is a wildcard/placeholder in modern Dart rather than an ordinary
	// identifier. Reserving it here also means the empty/punctuation-only
	// inputs sanitize() folds down to "_" (an `enum: ["", "asc"]` value, a
	// `<` comparison-operator enum value) pick up the same
	// trailing-underscore disambiguation every other keyword gets, becoming
	// "__".
	"_": true,
}

// sanitize maps an arbitrary JSON name onto a valid Dart identifier.
// The empty-string case assigns "_" rather than returning it, so it flows
// through the SAME digit-prefix and reserved-word checks as every other
// input — "_" is itself reserved (see reservedWords), so an empty name
// ends up as "__", not as the bare "_" Dart treats as a wildcard.
//
// A result that still starts with "_" at this point (from a name like
// "@odata.type" whose leading punctuation invalidIdentChar rewrote to
// "_", from a digit-leading name after the prefix above, or from the
// empty-string case's "__") gets "$" prepended: a leading underscore
// makes a Dart identifier library-private, which is a hard compile error
// on a named constructor parameter ("Named parameters can't start with
// an underscore") and, on a class, makes it unusable from any other file
// in modular output. "$" is itself a legal identifier-start character in
// Dart that carries no privacy meaning, so this neutralizes the problem
// without needing to re-run the digit-prefix or reserved-word checks
// above (neither can match a string that now starts with "$").
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
	if sanitized[0] == '_' {
		sanitized = "$" + sanitized
	}
	return sanitized
}

// SanitizeTypeIdentifier converts a model or synthesized type name into a
// valid Dart class identifier. Casing is left as-is.
func SanitizeTypeIdentifier(name string) string {
	return sanitize(name)
}

// SanitizeFieldIdentifier converts a JSON field name into a valid,
// idiomatic Dart property identifier.
func SanitizeFieldIdentifier(name string) string {
	return sanitize(mobile.ToCamelCase(name))
}

// enumMemberReservedNames are identifiers that would collide with a
// member every declaration renderEnum generates already carries: the
// explicit `final String value;` field this generator adds to every
// enum, plus the `index` and `values` members Dart's built-in Enum base
// class contributes to every enum automatically. These are not Dart
// language keywords (see reservedWords) — a class field or a type named
// "value" is completely fine — so they only apply to an enum VALUE
// identifier, never to SanitizeTypeIdentifier/SanitizeFieldIdentifier.
var enumMemberReservedNames = map[string]bool{
	"value": true, "index": true, "values": true,
}

// SanitizeEnumValueIdentifier converts a JSON enum value into a valid,
// idiomatic Dart enum value identifier (lowerCamelCase — Dart's official
// style guide uses lowerCamelCase for enum values, unlike Kotlin/Java's
// SCREAMING_SNAKE_CASE convention). A result that collides with
// enumMemberReservedNames gets an underscore appended, the same way
// sanitize already disambiguates a bare Dart keyword.
func SanitizeEnumValueIdentifier(value string) string {
	sanitized := sanitize(mobile.ToCamelCase(value))
	if enumMemberReservedNames[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// FileName converts a Dart class name into its conventional snake_case
// file name (TreeNode -> tree_node.dart), independent of the class name's
// own PascalCase spelling.
func FileName(typeName string) string {
	var sb strings.Builder
	runes := []rune(typeName)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteRune('_')
			}
			sb.WriteRune(r - 'A' + 'a')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String() + ".dart"
}
