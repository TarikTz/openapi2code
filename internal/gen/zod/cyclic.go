package zod

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/ts"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// renderCyclicModel renders a cyclic model as a hand-written TS type plus
// a z.lazy()-wrapped, explicitly z.ZodType<X>-annotated Zod schema — the
// standard Zod v3 pattern for self-referential schemas, where plain
// z.infer doesn't work. The TS type is rendered independently of the
// separate TS generator's output (see renderTSType below); this keeps the
// Zod generator's output file self-contained.
func (r *renderer) renderCyclicModel(name string, node *ir.Node) (ModelOutput, error) {
	tsExpr, tsDeps := r.renderTSType(node)
	zodExpr, zodDeps := r.renderZodType(node)
	deps := append(append([]string{}, tsDeps...), zodDeps...)
	decl := fmt.Sprintf(
		"export type %s = %s;\nexport const %sSchema: z.ZodType<%s> = z.lazy(() => %s);\n",
		name, tsExpr, name, name, zodExpr,
	)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}, nil
}

// renderTSType renders node as a bare TypeScript type expression. This
// mirrors internal/gen/ts's rendering rules (same combinator-wrapping,
// same field optional/nullable syntax, same wire-name-preserving field
// quoting via ts.QuoteFieldName) but is a separate, self-contained
// implementation — it exists only to give a cyclic model's z.ZodType<X>
// annotation something to point at, not to duplicate the TS generator's
// output files.
func (r *renderer) renderTSType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderTSPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := r.nameFor(node.RefName)
		return name, []string{name}
	case ir.KindEnum:
		return renderTSEnum(node.Enum), nil
	case ir.KindArray:
		return r.renderTSArray(node.Array)
	case ir.KindUnion:
		return r.renderTSVariantList(node.Union.Variants, " | ")
	case ir.KindIntersection:
		return r.renderTSVariantList(node.Intersection.Members, " & ")
	case ir.KindObject:
		return r.renderTSObject(node.Object)
	case ir.KindMap:
		return r.renderTSMap(node.Map)
	default:
		return "unknown", nil
	}
}

// renderTSMap mirrors internal/gen/ts's renderMap: Record<string, V> is a
// single generic type reference, needing no combinator-wrapping when
// embedded in an array or another combinator.
func (r *renderer) renderTSMap(m *ir.MapNode) (string, []string) {
	valuesExpr, deps := r.renderTSType(m.Values)
	return "Record<string, " + valuesExpr + ">", deps
}

func renderTSPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "string"
	case ir.PrimitiveNumber, ir.PrimitiveInteger:
		return "number"
	case ir.PrimitiveBoolean:
		return "boolean"
	default:
		return "unknown"
	}
}

func renderTSEnum(e *ir.EnumNode) string {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		if e.Primitive == ir.PrimitiveString {
			parts[i] = strconv.Quote(v)
		} else {
			parts[i] = v
		}
	}
	return strings.Join(parts, " | ")
}

func (r *renderer) renderTSArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := r.renderTSType(a.Items)
	return wrapForTSCombinator(itemExpr, a.Items) + "[]", deps
}

func (r *renderer) renderTSVariantList(nodes []*ir.Node, sep string) (string, []string) {
	if len(nodes) == 0 {
		return "unknown", nil
	}
	parts := make([]string, len(nodes))
	var deps []string
	for i, n := range nodes {
		expr, d := r.renderTSType(n)
		parts[i] = wrapForTSCombinator(expr, n)
		deps = append(deps, d...)
	}
	return strings.Join(parts, sep), deps
}

func (r *renderer) renderTSObject(o *ir.ObjectNode) (string, []string) {
	var deps []string
	var parts []string
	for _, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		parts = append(parts, name)
	}
	if len(parts) == 0 || len(o.Fields) > 0 {
		fieldsExpr, fieldDeps := r.renderTSFields(o.Fields)
		deps = append(deps, fieldDeps...)
		parts = append(parts, fieldsExpr)
	}
	return strings.Join(parts, " & "), deps
}

func (r *renderer) renderTSFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := r.renderTSType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForTSCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("%s%s: %s;", ts.QuoteFieldName(f.Name), optional, typeExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

func wrapForTSCombinator(expr string, n *ir.Node) string {
	switch n.Kind {
	case ir.KindUnion, ir.KindIntersection:
		return "(" + expr + ")"
	case ir.KindObject:
		if n.Object != nil && len(n.Object.Extends) > 0 {
			return "(" + expr + ")"
		}
	case ir.KindEnum:
		// See ts/generate.go's wrapForCombinator: TypeScript's postfix []
		// and & both bind tighter than the enum's bare "a" | "b" union, so
		// it needs parens unless it's a single value with no internal |.
		if n.Enum != nil && len(n.Enum.Values) > 1 {
			return "(" + expr + ")"
		}
	}
	return expr
}
