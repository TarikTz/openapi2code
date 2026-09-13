// Package mobile holds naming helpers shared by the Swift, Kotlin, and
// Dart generators: converting a JSON field or enum-value name into the
// idiomatic casing each language's conventions expect.
package mobile

import "strings"

// ToCamelCase converts a JSON field or enum-value name (commonly
// snake_case or kebab-case in real-world APIs) into idiomatic
// lowerCamelCase, as Swift, Kotlin, and Dart properties and Dart/Swift
// enum cases all use. A name with no separators passes through with only
// its first rune lowercased, so an already-camelCase or PascalCase input
// is handled correctly too.
func ToCamelCase(name string) string {
	parts := splitWords(name)
	if len(parts) == 0 {
		return name
	}
	var sb strings.Builder
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		if i == 0 {
			sb.WriteString(strings.ToLower(string(runes[0])))
			sb.WriteString(string(runes[1:]))
		} else {
			sb.WriteString(strings.ToUpper(string(runes[0])))
			sb.WriteString(strings.ToLower(string(runes[1:])))
		}
	}
	return sb.String()
}

// ToPascalCase converts a JSON field or model name into idiomatic
// UpperCamelCase, used for synthesized type names (combining a model
// name with a field name to name an inline enum or object).
func ToPascalCase(name string) string {
	camel := ToCamelCase(name)
	if camel == "" {
		return camel
	}
	runes := []rune(camel)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// ToScreamingSnakeCase converts a JSON enum value into idiomatic
// SCREAMING_SNAKE_CASE, as Kotlin enum constants conventionally use.
func ToScreamingSnakeCase(name string) string {
	var sb strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '-' || r == ' ' || r == '_':
			sb.WriteRune('_')
		case r >= 'A' && r <= 'Z' && i > 0 && isLowerOrDigit(runes[i-1]):
			sb.WriteRune('_')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return strings.ToUpper(sb.String())
}

func isLowerOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// splitWords breaks name on underscores, hyphens, and spaces into its
// constituent words, discarding empty segments from consecutive
// separators.
func splitWords(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
}
