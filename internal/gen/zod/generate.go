package zod

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Zod schema declaration (plus its
// z.infer'd or hand-written TS type, per model).
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string

	// Cyclic mirrors ir.Model.Cyclic for this model. A cyclic model's Zod
	// body is wrapped in z.lazy(() => ...), so it reads its dependencies
	// only when the closure runs — consumers laying declarations out in a
	// single file (see pkg/engine's monolithic output) need this to know
	// which declarations actually impose an ordering constraint.
	Cyclic bool
}

// Generate renders every model in doc as a Zod schema declaration, in the
// same order as doc.Models (name-sorted, so output is deterministic).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	r := &renderer{
		names:      assignNames(doc.Models),
		extendable: computeExtendable(doc.Models),
	}
	outputs := make([]ModelOutput, 0, len(doc.Models))
	for _, m := range doc.Models {
		out, err := r.renderModel(m)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", m.Name, err)
		}
		out.Dependencies = removeName(out.Dependencies, out.Name)
		outputs = append(outputs, out)
	}
	return outputs, nil
}

// barrelName is the modular barrel file's base name. Reserved so a schema
// of the same name cannot claim index.ts and clobber the barrel.
const barrelName = "index"

// assignNames maps each model's IR name to the identifier its schema
// constant and inferred type are declared under. This intentionally
// mirrors internal/gen/ts's assignNames exactly (same SanitizeIdentifier
// call, same reserved barrel name, same _2/_3 collision suffixing in
// doc.Models order) so a spec generating both TS and Zod output gets
// identically-named models across targets.
func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{barrelName: true}
	names := make(map[string]string, len(models))
	for _, m := range models {
		base := ts.SanitizeIdentifier(m.Name)
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		used[name] = true
		names[m.Name] = name
	}
	return names
}

// renderer carries the model-name assignment so every reference to a
// model — its declaration and refs to it — uses the same uniquified
// identifier, plus a map of which model names can safely receive a
// .extend() call.
type renderer struct {
	names      map[string]string
	extendable map[string]bool
}

// computeExtendable reports, per raw IR model name, whether that model's
// generated schema is a ZodObject and can therefore receive .extend().
//
// A model is extendable only if it is object-shaped (anything else — an
// enum, a union, a primitive alias — never renders to a ZodObject), is not
// itself cyclic (a cyclic model renders as z.lazy(), typed z.ZodType<X>,
// which has no .extend()), and every model it extends is extendable too.
// The last clause is what makes this transitive: a model that extends a
// cyclic base renders as a z.intersection, so anything extending *it* must
// fall back to an intersection as well, and so on down the chain.
//
// Results are memoized. The Extends-only graph is provably acyclic here (a
// genuine cycle among Extends edges makes every participant Cyclic, which
// is excluded above), but memoizing both avoids the exponential re-walk of
// diamond-shaped extends graphs and keeps a hypothetical cycle from
// recursing forever — the provisional false written before recursing is
// the guard.
func computeExtendable(models []*ir.Model) map[string]bool {
	byName := make(map[string]*ir.Model, len(models))
	for _, m := range models {
		byName[m.Name] = m
	}
	memo := make(map[string]bool, len(models))

	var visit func(name string) bool
	visit = func(name string) bool {
		if v, ok := memo[name]; ok {
			return v
		}
		m, ok := byName[name]
		if !ok {
			// A name that belongs to no model in this document tells us
			// nothing; assume extendable, which is exactly how such names
			// have always been rendered.
			return true
		}
		memo[name] = false // provisional, so a re-entrant walk terminates
		result := !m.Cyclic && m.Type != nil && m.Type.Kind == ir.KindObject && m.Type.Object != nil
		if result {
			for _, e := range m.Type.Object.Extends {
				if !visit(e) {
					result = false
					break
				}
			}
		}
		memo[name] = result
		return result
	}

	for _, m := range models {
		visit(m.Name)
	}
	return memo
}

// nameFor returns the identifier a model name is declared under. Names
// not belonging to a model in the document fall back to plain
// sanitization.
func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return ts.SanitizeIdentifier(modelName)
}

func (r *renderer) renderModel(m *ir.Model) (ModelOutput, error) {
	name := r.nameFor(m.Name)
	if m.Cyclic {
		out, err := r.renderCyclicModel(name, m.Type)
		if err != nil {
			return ModelOutput{}, err
		}
		out.Cyclic = true
		return out, nil
	}
	expr, deps := r.renderZodType(m.Type)
	decl := fmt.Sprintf("export const %sSchema = %s;\nexport type %s = z.infer<typeof %sSchema>;\n", name, expr, name, name)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}, nil
}

// isExtendable reports whether modelName's schema can receive .extend().
// Names not belonging to any model in the document are assumed extendable
// — they carry no information, and this preserves how partial documents
// have always rendered.
func (r *renderer) isExtendable(modelName string) bool {
	if v, ok := r.extendable[modelName]; ok {
		return v
	}
	return true
}

func (r *renderer) renderZodType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := r.nameFor(node.RefName)
		return name + "Schema", []string{name}
	case ir.KindEnum:
		return renderEnum(node.Enum), nil
	case ir.KindArray:
		return r.renderArray(node.Array)
	case ir.KindUnion:
		return r.renderUnion(node.Union)
	case ir.KindIntersection:
		return r.renderIntersection(node.Intersection)
	case ir.KindObject:
		return r.renderObject(node.Object)
	case ir.KindMap:
		return r.renderRecord(node.Map)
	default:
		return "z.unknown()", nil
	}
}

// renderRecord renders a dictionary type (from additionalProperties) as
// Zod v3's single-argument z.record(valueSchema) — this project targets
// zod@^3 (see pkg/engine/golden_zod_typecheck_test.go), whose
// single-argument form already defaults the key schema to string.
func (r *renderer) renderRecord(m *ir.MapNode) (string, []string) {
	valuesExpr, deps := r.renderZodType(m.Values)
	return fmt.Sprintf("z.record(%s)", valuesExpr), deps
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "z.string()"
	case ir.PrimitiveNumber, ir.PrimitiveInteger:
		return "z.number()"
	case ir.PrimitiveBoolean:
		return "z.boolean()"
	default:
		return "z.unknown()"
	}
}

func renderEnum(e *ir.EnumNode) string {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		parts[i] = strconv.Quote(v)
	}
	return fmt.Sprintf("z.enum([%s])", strings.Join(parts, ", "))
}

func (r *renderer) renderArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := r.renderZodType(a.Items)
	return fmt.Sprintf("z.array(%s)", itemExpr), deps
}

// renderUnion renders oneOf/anyOf. A single-variant union (legal but
// unusual) is rendered as that variant directly — z.union's TS signature
// requires at least two members in its tuple type.
func (r *renderer) renderUnion(u *ir.UnionNode) (string, []string) {
	if len(u.Variants) == 1 {
		return r.renderZodType(u.Variants[0])
	}
	parts := make([]string, len(u.Variants))
	var deps []string
	for i, v := range u.Variants {
		expr, d := r.renderZodType(v)
		parts[i] = expr
		deps = append(deps, d...)
	}
	return fmt.Sprintf("z.union([%s])", strings.Join(parts, ", ")), deps
}

// renderIntersection renders the allOf-with-non-object-member fallback,
// folding 3+ members left-to-right: z.intersection(z.intersection(a, b), c).
func (r *renderer) renderIntersection(in *ir.IntersectionNode) (string, []string) {
	if len(in.Members) == 0 {
		return "z.unknown()", nil
	}
	expr, deps := r.renderZodType(in.Members[0])
	for _, m := range in.Members[1:] {
		next, d := r.renderZodType(m)
		deps = append(deps, d...)
		expr = fmt.Sprintf("z.intersection(%s, %s)", expr, next)
	}
	return expr, deps
}

// renderObject renders an object schema. Extends (from allOf) chains
// .extend() calls when every base is extendable (see computeExtendable),
// and otherwise folds to z.intersection — a base that renders as
// z.ZodType<X> (cyclic), as a z.intersection (extends a cyclic base
// itself), or as anything else non-object has no .extend() method.
func (r *renderer) renderObject(o *ir.ObjectNode) (string, []string) {
	var deps []string
	for _, e := range o.Extends {
		if !r.isExtendable(e) {
			return r.renderObjectAsIntersection(o)
		}
	}
	// Non-cyclic path: use .extend() chaining
	base := ""
	for i, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		if i == 0 {
			base = name + "Schema"
		} else {
			base = fmt.Sprintf("%s.extend(%sSchema.shape)", base, name)
		}
	}
	fieldsExpr, fieldDeps := r.renderFields(o.Fields)
	deps = append(deps, fieldDeps...)
	if base == "" {
		return fmt.Sprintf("z.object(%s)", fieldsExpr), deps
	}
	if len(o.Fields) == 0 {
		return base, deps
	}
	return fmt.Sprintf("%s.extend(%s)", base, fieldsExpr), deps
}

// renderObjectAsIntersection renders an object that has at least one
// non-extendable Extends target. Instead of .extend(), it folds the bases
// and the object's own fields together with z.intersection, which works
// against any schema type — including z.ZodType<X> and another
// z.intersection.
func (r *renderer) renderObjectAsIntersection(o *ir.ObjectNode) (string, []string) {
	var deps []string
	var members []string
	for _, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		members = append(members, name+"Schema")
	}
	fieldsExpr, fieldDeps := r.renderFields(o.Fields)
	deps = append(deps, fieldDeps...)
	if len(o.Fields) > 0 {
		members = append(members, fmt.Sprintf("z.object(%s)", fieldsExpr))
	}
	if len(members) == 0 {
		return "z.object({})", deps
	}
	if len(members) == 1 {
		return members[0], deps
	}
	expr := members[0]
	for _, m := range members[1:] {
		expr = fmt.Sprintf("z.intersection(%s, %s)", expr, m)
	}
	return expr, deps
}

func (r *renderer) renderFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := r.renderZodType(f.Type)
		deps = append(deps, fieldDeps...)
		if f.Nullable {
			fieldExpr += ".nullable()"
		}
		if f.Optional {
			fieldExpr += ".optional()"
		}
		sb.WriteString(fmt.Sprintf("%s: %s,", ts.QuoteFieldName(f.Name), fieldExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

func dedupeSorted(names []string) []string {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func removeName(names []string, exclude string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != exclude {
			out = append(out, n)
		}
	}
	return out
}
