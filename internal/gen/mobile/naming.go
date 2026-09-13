package mobile

import (
	"fmt"

	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// Uniquify returns base, or base with a numeric suffix appended, so that
// the result is not already present in used (which it does not itself
// update — callers record the result themselves, since some also need to
// record it under a different key, as AssignNames does).
func Uniquify(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
}

// AssignNames maps every model's IR name to a unique target-language type
// identifier: sanitize turns a raw schema name into that language's valid
// (but not yet unique) identifier form, and Uniquify resolves any
// collision that sanitizing two different schema names onto the same
// identifier would otherwise cause.
func AssignNames(models []*ir.Model, sanitize func(string) string) map[string]string {
	used := map[string]bool{}
	names := make(map[string]string, len(models))
	for _, m := range models {
		name := Uniquify(sanitize(m.Name), used)
		used[name] = true
		names[m.Name] = name
	}
	return names
}
