package ts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level TypeScript declaration.
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
}

// Generate renders every model in doc as a TypeScript declaration, in the
// same order as doc.Models (name-sorted, so output is deterministic).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	r := &renderer{names: assignNames(doc.Models)}
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

// barrelName is the modular barrel file's base name. It is reserved so a
// schema of the same name cannot claim index.ts and clobber the barrel.
const barrelName = "index"

// assignNames maps each model's IR name to the TypeScript identifier it is
// declared under. Distinct schema names can sanitize to the same
// identifier (e.g. "Pet.Status" and "Pet-Status" both give "Pet_Status");
// colliding names get a _2, _3, ... suffix, assigned in doc.Models order
// (name-sorted) so the result is deterministic. Without this, a monolithic
// build emits duplicate declarations and a modular build silently drops a
// whole model.
func assignNames(models []*ir.Model) map[string]string {
	used := map[string]bool{barrelName: true}
	names := make(map[string]string, len(models))
	for _, m := range models {
		base := SanitizeIdentifier(m.Name)
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		used[name] = true
		names[m.Name] = name
	}
	return names
}

// renderer carries the model-name assignment so every reference to a model
// — its declaration, refs to it, and extends clauses — uses the same
// uniquified identifier.
type renderer struct {
	names map[string]string
}

// nameFor returns the TypeScript identifier a model name is declared under.
// Names not belonging to a model in the document (which the IR builder does
// not produce) fall back to plain sanitization.
func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeIdentifier(modelName)
}

func (r *renderer) renderModel(m *ir.Model) (ModelOutput, error) {
	name := r.nameFor(m.Name)
	if m.Type.Kind == ir.KindObject {
		return r.renderInterface(name, m.Type.Object), nil
	}
	return r.renderTypeAlias(name, m.Type), nil
}

func (r *renderer) renderInterface(name string, o *ir.ObjectNode) ModelOutput {
	var sb strings.Builder
	var deps []string
	sb.WriteString("export interface ")
	sb.WriteString(name)
	if len(o.Extends) > 0 {
		extendNames := make([]string, len(o.Extends))
		for i, e := range o.Extends {
			extendNames[i] = r.nameFor(e)
		}
		deps = append(deps, extendNames...)
		sb.WriteString(" extends ")
		sb.WriteString(strings.Join(extendNames, ", "))
	}
	sb.WriteString(" {\n")
	for _, f := range o.Fields {
		fieldExpr, fieldDeps := r.renderType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", QuoteFieldName(f.Name), optional, typeExpr))
	}
	sb.WriteString("}\n")
	return ModelOutput{Name: name, Declaration: sb.String(), Dependencies: dedupeSorted(deps)}
}

func (r *renderer) renderTypeAlias(name string, node *ir.Node) ModelOutput {
	expr, deps := r.renderType(node)
	decl := fmt.Sprintf("export type %s = %s;\n", name, expr)
	return ModelOutput{Name: name, Declaration: decl, Dependencies: dedupeSorted(deps)}
}

func (r *renderer) renderType(node *ir.Node) (string, []string) {
	switch node.Kind {
	case ir.KindPrimitive:
		return renderPrimitive(node.Primitive), nil
	case ir.KindRef:
		name := r.nameFor(node.RefName)
		return name, []string{name}
	case ir.KindEnum:
		return renderEnum(node.Enum)
	case ir.KindArray:
		return r.renderArray(node.Array)
	case ir.KindUnion:
		return r.renderVariantList(node.Union.Variants, " | ")
	case ir.KindIntersection:
		return r.renderVariantList(node.Intersection.Members, " & ")
	case ir.KindObject:
		return r.renderInlineObject(node.Object)
	case ir.KindMap:
		return r.renderMap(node.Map)
	default:
		return "unknown", nil
	}
}

// renderMap renders a dictionary type (from additionalProperties) as
// TypeScript's Record utility type. Record<string, V>[] and
// Record<string, V> | X both parse unambiguously with no wrapping
// needed — Record<...> is a single generic type reference, unlike a
// bare union or an object carrying Extends.
func (r *renderer) renderMap(m *ir.MapNode) (string, []string) {
	valuesExpr, deps := r.renderType(m.Values)
	return "Record<string, " + valuesExpr + ">", deps
}

func renderPrimitive(p ir.Primitive) string {
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

func renderEnum(e *ir.EnumNode) (string, []string) {
	parts := make([]string, len(e.Values))
	for i, v := range e.Values {
		parts[i] = strconv.Quote(v)
	}
	return strings.Join(parts, " | "), nil
}

func (r *renderer) renderArray(a *ir.ArrayNode) (string, []string) {
	itemExpr, deps := r.renderType(a.Items)
	return wrapForCombinator(itemExpr, a.Items) + "[]", deps
}

func (r *renderer) renderVariantList(nodes []*ir.Node, sep string) (string, []string) {
	parts := make([]string, len(nodes))
	var deps []string
	for i, n := range nodes {
		expr, d := r.renderType(n)
		parts[i] = wrapForCombinator(expr, n)
		deps = append(deps, d...)
	}
	return strings.Join(parts, sep), deps
}

// renderInlineObject renders a nested (non-top-level) object type as an
// inline TS type expression. An Extends list on a nested object (from a
// property whose own schema uses a multi-member allOf) has no inline
// equivalent to `interface ... extends ...`, so it is rendered as an
// intersection instead: `Base & { extra?: string; }`, or just `Base` when
// the object contributes no fields of its own. Top-level
// allOf-with-extends is handled by renderInterface.
func (r *renderer) renderInlineObject(o *ir.ObjectNode) (string, []string) {
	var deps []string
	var parts []string
	for _, e := range o.Extends {
		name := r.nameFor(e)
		deps = append(deps, name)
		parts = append(parts, name)
	}
	if len(parts) == 0 || len(o.Fields) > 0 {
		fieldsExpr, fieldDeps := r.renderInlineObjectFields(o.Fields)
		deps = append(deps, fieldDeps...)
		parts = append(parts, fieldsExpr)
	}
	return strings.Join(parts, " & "), deps
}

func (r *renderer) renderInlineObjectFields(fields []*ir.Field) (string, []string) {
	var sb strings.Builder
	var deps []string
	sb.WriteString("{ ")
	for i, f := range fields {
		if i > 0 {
			sb.WriteString(" ")
		}
		fieldExpr, fieldDeps := r.renderType(f.Type)
		deps = append(deps, fieldDeps...)
		optional := ""
		if f.Optional {
			optional = "?"
		}
		typeExpr := fieldExpr
		if f.Nullable {
			typeExpr = wrapForCombinator(fieldExpr, f.Type) + " | null"
		}
		sb.WriteString(fmt.Sprintf("%s%s: %s;", QuoteFieldName(f.Name), optional, typeExpr))
	}
	sb.WriteString(" }")
	return sb.String(), deps
}

// wrapForCombinator parens-wraps a rendered type expression when it needs
// grouping to be embedded correctly inside an array (`(A | B)[]`) or
// another combinator (`(A | B) | null`). An inline object carrying Extends
// renders as an intersection, so it needs the same grouping.
func wrapForCombinator(expr string, n *ir.Node) string {
	switch n.Kind {
	case ir.KindUnion, ir.KindIntersection:
		return "(" + expr + ")"
	case ir.KindObject:
		if n.Object != nil && len(n.Object.Extends) > 0 {
			return "(" + expr + ")"
		}
	case ir.KindEnum:
		// A multi-value enum renders as a bare "a" | "b" union: TypeScript's
		// postfix [] and & both bind tighter than |, so embedding it inside
		// an array or intersection without parens changes its meaning (e.g.
		// `"a" | "b"[]` parses as `"a" | ("b"[])`, not `("a" | "b")[]`). A
		// single-value enum has no internal | and needs no parens.
		if n.Enum != nil && len(n.Enum.Values) > 1 {
			return "(" + expr + ")"
		}
	}
	return expr
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
