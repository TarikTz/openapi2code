package ts

import (
	"regexp"
	"strconv"
)

var invalidIdentChar = regexp.MustCompile(`[^A-Za-z0-9_$]`)

// reservedWords are TypeScript/JavaScript keywords that cannot be used as
// a type name. They are fine as (quoted or unquoted) property names, so
// only SanitizeIdentifier consults this set.
var reservedWords = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "debugger": true, "default": true, "delete": true,
	"do": true, "else": true, "enum": true, "export": true, "extends": true,
	"false": true, "finally": true, "for": true, "function": true, "if": true,
	"implements": true, "import": true, "in": true, "instanceof": true,
	"interface": true, "let": true, "new": true, "null": true, "package": true,
	"private": true, "protected": true, "public": true, "return": true,
	"static": true, "super": true, "switch": true, "this": true, "throw": true,
	"true": true, "try": true, "type": true, "typeof": true, "var": true,
	"void": true, "while": true, "with": true, "yield": true,
}

// SanitizeIdentifier converts an arbitrary schema name into a valid
// TypeScript identifier: invalid characters become underscores, a leading
// digit gets an underscore prefix, and a reserved word gets a trailing
// underscore. It is only for names that must be bare identifiers in TS
// syntax — type/interface names and the names used in extends and ref
// positions. Object property names must NOT go through it; use
// quoteFieldName instead, which preserves the wire name.
func SanitizeIdentifier(name string) string {
	sanitized := invalidIdentChar.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	if reservedWords[sanitized] {
		sanitized += "_"
	}
	return sanitized
}

// isValidIdentifier reports whether name can be used as a bare, unquoted
// TypeScript identifier (ignoring reserved words, which are still legal as
// property names).
func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	return !invalidIdentChar.MatchString(name)
}

// QuoteFieldName renders an object property name. Property names are part
// of the wire contract, so they are never renamed: a name that is not a
// bare-identifier-safe string is quoted (`"content-type"`) rather than
// sanitized, which would silently change the field a consumer must read.
// Exported for reuse by other generator packages (e.g. zod) that need the
// same wire-fidelity guarantee.
func QuoteFieldName(name string) string {
	if isValidIdentifier(name) {
		return name
	}
	return strconv.Quote(name)
}
