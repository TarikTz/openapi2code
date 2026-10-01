// internal/gen/python/generate.go
package python

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// Style selects which Python flavor Generate renders: plain stdlib
// dataclasses, or Pydantic BaseModel subclasses. The two share every
// type-mapping, naming, and declaration-ordering rule below, differing
// only in class header, field default/alias syntax, and imports (see
// renderObjectDecl).
type Style int

const (
	Dataclass Style = iota
	Pydantic
)

// ModelOutput is one generated top-level Python declaration: a
// dataclass/BaseModel class, an Enum class, or a plain type-alias
// assignment. Dependencies lists the other declarations (by their
// resolved Python name) this one's Declaration references — needed
// because, like Dart (and unlike Swift/Kotlin), Python requires an
// explicit import between files in modular output. Imports lists the
// literal import lines (e.g. "from enum import Enum") this declaration
// alone needs, independent of Dependencies.
//
// Alias is true when Declaration is a module-level assignment (e.g.
// `AllPets = list[Pet]`) rather than a class or Enum. The distinction
// matters for declaration order within one file: `from __future__ import
// annotations` defers evaluation of a class's field annotations, so
// classes may reference each other (and aliases) in any order, but an
// assignment's right-hand side is evaluated the moment the module runs
// that line — every name in Dependencies must already be defined above
// it, or importing the module raises NameError.
//
// Unsupported is true when Declaration is only the explanatory comment
// emitted for a schema shape with no Python representation; it defines
// no Python name, so nothing may import or re-export Name.
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
	Imports      []string
	Alias        bool
	Unsupported  bool
}

type extraDecl = mobile.PendingDecl

// typeResult is resolveType's return: a Python type expression plus
// everything a caller building a ModelOutput needs to aggregate across
// every field of an object (see resolveFields).
type typeResult struct {
	Expr    string
	Skip    bool
	Deps    []string
	Imports []string
}

// renderer carries the state shared by every method that walks the IR
// for one Generate call — same shape and purpose as
// internal/gen/swift/kotlin/dart's own renderer structs.
type renderer struct {
	names        map[string]string
	modelsByName map[string]*ir.Model
	used         map[string]bool
	style        Style
}

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

// resolveType resolves node to a non-optional Python type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object — see
// internal/gen/swift/generate.go's resolveType doc comment for why the
// synthesized name must be minted exactly once, here, rather than
// recomputed later.
func (r *renderer) resolveType(node *ir.Node, suggestedName string) (typeResult, []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return typeResult{Skip: true}, nil
		}
		return typeResult{Expr: renderPrimitive(node.Primitive)}, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName, map[string]bool{}) {
			return typeResult{Skip: true}, nil
		}
		target := r.nameFor(node.RefName)
		return typeResult{Expr: target, Deps: []string{target}}, nil
	case ir.KindArray:
		inner, extra := r.resolveType(node.Array.Items, suggestedName)
		if inner.Skip {
			return typeResult{Skip: true}, nil
		}
		return typeResult{Expr: "list[" + inner.Expr + "]", Deps: inner.Deps, Imports: inner.Imports}, extra
	case ir.KindMap:
		inner, extra := r.resolveType(node.Map.Values, suggestedName)
		if inner.Skip {
			return typeResult{Skip: true}, nil
		}
		return typeResult{Expr: "dict[str, " + inner.Expr + "]", Deps: inner.Deps, Imports: inner.Imports}, extra
	case ir.KindEnum:
		// A Python Enum mixing in str/int/float needs its members to
		// actually be that type; Bool has no useful Enum mixin (and
		// Python's bool is already a tiny closed set), so a boolean enum
		// bypasses synthesizing a named declaration entirely and
		// resolves straight to bool — mirroring
		// internal/gen/swift/generate.go's identical bypass for the
		// identical reason.
		if node.Enum.Primitive == ir.PrimitiveBoolean {
			return typeResult{Expr: "bool"}, nil
		}
		name := mobile.Uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		return typeResult{Expr: name, Deps: []string{name}}, []extraDecl{{Name: name, Node: node}}
	case ir.KindObject:
		name := mobile.Uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		return typeResult{Expr: name, Deps: []string{name}}, []extraDecl{{Name: name, Node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return typeResult{Skip: true}, nil
	default:
		return typeResult{Skip: true}, nil
	}
}

// refIsUnsupported reports whether the model named modelName would
// render as an unsupportedShapeComment rather than a real declaration.
// Mirrors internal/gen/swift/generate.go's refIsUnsupported exactly.
func (r *renderer) refIsUnsupported(modelName string, visited map[string]bool) bool {
	if visited[modelName] {
		return true
	}
	visited[modelName] = true
	m, ok := r.modelsByName[modelName]
	if !ok || m.Type == nil {
		return true
	}
	return r.nodeIsUnsupported(m.Type, visited)
}

// nodeIsUnsupported mirrors internal/gen/swift/generate.go's
// nodeIsUnsupported exactly — Python, like Swift, needs no custom
// serialization dispatch, so there is no Dart-style
// itemDispatchIsUnsupported wrinkle here.
func (r *renderer) nodeIsUnsupported(node *ir.Node, visited map[string]bool) bool {
	switch node.Kind {
	case ir.KindRef:
		return r.refIsUnsupported(node.RefName, visited)
	case ir.KindPrimitive:
		return node.Primitive == ir.PrimitiveUnknown
	case ir.KindArray:
		return r.nodeIsUnsupported(node.Array.Items, visited)
	case ir.KindMap:
		return r.nodeIsUnsupported(node.Map.Values, visited)
	case ir.KindUnion, ir.KindIntersection:
		return true
	default: // KindEnum, KindObject: resolveType always synthesizes a real declaration for these
		return false
	}
}

// unsupportedShapeComment renders the comment-only declaration emitted in
// place of name's real declaration when its shape has no clean Python
// representation. Wording matches Swift/Kotlin/Dart's identical helper.
func unsupportedShapeComment(name string) string {
	return fmt.Sprintf("# %s was not generated for the Python target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", commentSafe(name))
}

// commentSafe makes s safe to interpolate into a single-line `#` comment.
// JSON property names may legally contain line breaks, and the spec may
// come from an untrusted remote URL: a raw "\n" would end the comment and
// let the rest of the name run as Python code when the generated module is
// imported. Every control character (which covers \n, \r, NUL — a NUL
// byte makes the whole file unparseable — and the C1 range including
// U+0085) and the Unicode line/paragraph separators U+2028/U+2029 is
// replaced with a space, so the comment always stays on one physical line.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "str"
	case ir.PrimitiveNumber:
		return "float"
	case ir.PrimitiveInteger:
		return "int"
	case ir.PrimitiveBoolean:
		return "bool"
	default:
		return "str"
	}
}

// pythonEnumBacking returns the Python Enum mixin base class backing an
// enum for p, plus a function rendering one enum value as that type's
// literal syntax. Mirrors internal/gen/swift/generate.go's
// swiftEnumBacking, adapted to Python's (str, Enum)/(int, Enum)/(float,
// Enum) mixin convention. PrimitiveBoolean never reaches here (see
// resolveType's and renderDeclaration's boolean-enum bypass).
func pythonEnumBacking(p ir.Primitive) (baseClass string, formatLiteral func(v string) string) {
	switch p {
	case ir.PrimitiveInteger, ir.PrimitiveNumber:
		return renderPrimitive(p), func(v string) string { return v }
	default:
		return "str", func(v string) string { return strconv.Quote(v) }
	}
}

// renderEnumDecl renders e as a Python Enum subclass. Member identifiers
// are uniquified within this one enum (used is local to this call) for
// the same reason internal/gen/swift/generate.go's renderEnumDecl
// uniquifies case identifiers: two different wire values can converge on
// the same identifier under SanitizeEnumMemberIdentifier.
func renderEnumDecl(name string, e *ir.EnumNode) string {
	baseClass, formatLiteral := pythonEnumBacking(e.Primitive)
	var sb strings.Builder
	fmt.Fprintf(&sb, "class %s(%s, Enum):\n", name, baseClass)
	used := make(map[string]bool, len(e.Values))
	for _, v := range e.Values {
		memberName := mobile.Uniquify(SanitizeEnumMemberIdentifier(v), used)
		used[memberName] = true
		fmt.Fprintf(&sb, "    %s = %s\n", memberName, formatLiteral(v))
	}
	return sb.String()
}

// resolvedField is one field ready to render into an object's body.
type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	optional     bool
	aliased      bool // true when propertyName != wireName (Pydantic needs Field(alias=...))
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the project-wide "allOf always flattens" rule — see
// internal/gen/swift/generate.go's flattenFields doc comment for the
// full rationale (identical here; Python dataclasses/BaseModels model no
// inheritance in this generator either).
func (r *renderer) flattenFields(o *ir.ObjectNode) []*ir.Field {
	ownNames := make(map[string]bool, len(o.Fields))
	for _, f := range o.Fields {
		ownNames[f.Name] = true
	}
	seen := make(map[string]bool)
	var fields []*ir.Field
	addBase := func(fs []*ir.Field) {
		for _, f := range fs {
			if ownNames[f.Name] || seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}
	visited := make(map[string]bool, len(o.Extends))
	for _, e := range o.Extends {
		addBase(r.flattenExtendsTarget(e, visited))
	}
	return append(fields, o.Fields...)
}

// flattenExtendsTarget mirrors internal/gen/swift/generate.go's
// flattenExtendsTarget exactly (see its doc comment for the full
// rationale, including the $ref-to-object alias-chain handling).
func (r *renderer) flattenExtendsTarget(modelName string, visited map[string]bool) []*ir.Field {
	if visited[modelName] {
		return nil
	}
	visited[modelName] = true
	m, ok := r.modelsByName[modelName]
	if !ok || m.Type == nil {
		return nil
	}
	for m.Type.Kind == ir.KindRef {
		next, ok := r.modelsByName[m.Type.RefName]
		if !ok || visited[m.Type.RefName] {
			return nil
		}
		visited[m.Type.RefName] = true
		m = next
		if m.Type == nil {
			return nil
		}
	}
	if m.Type.Kind != ir.KindObject {
		return nil
	}
	ownNames := make(map[string]bool, len(m.Type.Object.Fields))
	for _, f := range m.Type.Object.Fields {
		ownNames[f.Name] = true
	}
	seen := make(map[string]bool)
	var fields []*ir.Field
	for _, e := range m.Type.Object.Extends {
		for _, f := range r.flattenExtendsTarget(e, visited) {
			if ownNames[f.Name] || seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}
	return append(fields, m.Type.Object.Fields...)
}

// resolveFields resolves every field of an object into resolvedFields,
// aggregating dependencies, imports, and synthesized declarations along
// the way. skipped carries the wire name of every field omitted because
// resolveType found no representable Python type for it. Property
// identifiers are uniquified within this one object, same reasoning as
// internal/gen/swift/generate.go's resolveFields.
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) (resolved []resolvedField, deps []string, imports []string, allExtra []extraDecl, skipped []string) {
	used := make(map[string]bool, len(fields))
	// plainNames holds every field identifier that needs no Pydantic
	// underscore rewrite (see pydanticPublicFieldName).
	plainNames := make(map[string]bool, len(fields))
	for _, f := range fields {
		if n := SanitizeFieldIdentifier(f.Name); !strings.HasPrefix(n, "_") {
			plainNames[n] = true
		}
	}
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		res, extra := r.resolveType(f.Type, suggested)
		if res.Skip {
			skipped = append(skipped, f.Name)
			continue
		}
		optional := f.Optional || f.Nullable
		typeExpr := res.Expr
		if optional {
			typeExpr += " | None"
		}
		base := SanitizeFieldIdentifier(f.Name)
		taken := used
		if r.style == Pydantic && strings.HasPrefix(base, "_") {
			base = pydanticPublicFieldName(base)
			// A rewritten name yields to a field that sanitizes to it
			// naturally: with both "_id" and "id", "id" keeps its own
			// name and "_id" becomes "id_2", not the other way round.
			taken = make(map[string]bool, len(used)+len(plainNames))
			for n := range used {
				taken[n] = true
			}
			for n := range plainNames {
				taken[n] = true
			}
		}
		propertyName := mobile.Uniquify(base, taken)
		used[propertyName] = true
		resolved = append(resolved, resolvedField{
			propertyName: propertyName,
			wireName:     f.Name,
			typeExpr:     typeExpr,
			optional:     optional,
			aliased:      propertyName != f.Name,
		})
		deps = append(deps, res.Deps...)
		imports = append(imports, res.Imports...)
		allExtra = append(allExtra, extra...)
	}
	return resolved, deps, imports, allExtra, skipped
}

// pydanticPublicFieldName rewrites an already-sanitized field identifier
// so it does not start with an underscore. Pydantic v2 treats any
// underscore-prefixed class attribute as a private attribute rather than a
// model field — excluded from validation, serialization and the schema —
// so a wire field like "_id" (MongoDB) or "_links" (HAL) would otherwise
// vanish silently. The leading underscores are stripped ("_id" -> "id");
// if what remains is empty or starts with a digit (e.g. "2fa", which
// sanitizes to "_2fa"), it is prefixed with "field_" instead. The result
// always differs from the wire name, so the caller's existing alias
// machinery (Field(alias=...) plus populate_by_name) keeps the wire
// format intact. A collision with another field (e.g. both "_id" and
// "id") is resolved in resolveFields, which suffixes the rewritten name.
func pydanticPublicFieldName(name string) string {
	if !strings.HasPrefix(name, "_") {
		return name
	}
	stripped := strings.TrimLeft(name, "_")
	if stripped == "" {
		return "field"
	}
	if stripped[0] >= '0' && stripped[0] <= '9' {
		return "field_" + stripped
	}
	return sanitize(stripped)
}

// renderObjectDecl renders o as a Python class for the renderer's style.
// kw_only=True on the dataclass form sidesteps Python's "non-default
// argument follows default argument" rule entirely, regardless of field
// order. Reordering fields (required first) would also avoid that error,
// but would make generated field order diverge from the schema's declared
// order, which every other target preserves.
func (r *renderer) renderObjectDecl(name string, o *ir.ObjectNode) (string, []string, []string, []extraDecl) {
	fields, deps, fieldImports, extra, skipped := r.resolveFields(name, r.flattenFields(o))
	anyAliased := false
	for _, f := range fields {
		if f.aliased {
			anyAliased = true
		}
	}

	var sb strings.Builder
	var imports []string
	switch r.style {
	case Pydantic:
		fmt.Fprintf(&sb, "class %s(BaseModel):\n", name)
		if anyAliased {
			imports = append(imports, "from pydantic import BaseModel, ConfigDict, Field")
		} else {
			imports = append(imports, "from pydantic import BaseModel")
		}
	default: // Dataclass
		sb.WriteString("@dataclass(kw_only=True)\n")
		fmt.Fprintf(&sb, "class %s:\n", name)
		imports = append(imports, "from dataclasses import dataclass")
	}

	// A class body with no real fields needs an explicit `pass`
	// regardless of whether there are skipped-field comments: unlike
	// Swift's empty-struct-is-valid case, a Python class body consisting
	// ONLY of comment lines is a SyntaxError ("expected an indented
	// block") — a comment is not a statement.
	if len(fields) == 0 {
		sb.WriteString("    pass\n")
	}
	for _, f := range fields {
		switch {
		case r.style == Pydantic && f.aliased && f.optional:
			fmt.Fprintf(&sb, "    %s: %s = Field(default=None, alias=%s)\n", f.propertyName, f.typeExpr, strconv.Quote(f.wireName))
		case r.style == Pydantic && f.aliased:
			fmt.Fprintf(&sb, "    %s: %s = Field(alias=%s)\n", f.propertyName, f.typeExpr, strconv.Quote(f.wireName))
		case f.optional:
			fmt.Fprintf(&sb, "    %s: %s = None\n", f.propertyName, f.typeExpr)
		default:
			fmt.Fprintf(&sb, "    %s: %s\n", f.propertyName, f.typeExpr)
		}
	}
	for _, wireName := range skipped {
		fmt.Fprintf(&sb, "    # %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", commentSafe(wireName))
	}
	if r.style == Pydantic && anyAliased {
		sb.WriteString("\n    model_config = ConfigDict(populate_by_name=True)\n")
	}

	return sb.String(), dedupeSorted(deps), dedupeSorted(append(imports, fieldImports...)), extra
}

// renderDeclaration renders node as a top-level Python declaration named
// name. Mirrors internal/gen/swift/generate.go's renderDeclaration
// structure; the ir.KindArray/ir.KindMap/ir.KindRef cases render a plain
// `Name = <expr>` assignment, which both serves as a valid runtime type
// alias and needs no "TypeAlias" import. alias reports whether decl is
// such an assignment (see ModelOutput.Alias for why callers care).
func (r *renderer) renderDeclaration(name string, node *ir.Node) (decl string, deps []string, imports []string, extra []extraDecl, alias bool) {
	switch node.Kind {
	case ir.KindObject:
		decl, deps, imports, extra = r.renderObjectDecl(name, node.Object)
		return decl, deps, imports, extra, false
	case ir.KindEnum:
		if node.Enum.Primitive == ir.PrimitiveBoolean {
			return fmt.Sprintf("%s = bool\n", name), nil, nil, nil, true
		}
		return renderEnumDecl(name, node.Enum), nil, []string{"from enum import Enum"}, nil, false
	case ir.KindUnion, ir.KindIntersection:
		return unsupportedShapeComment(name), nil, nil, nil, false
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return unsupportedShapeComment(name), nil, nil, nil, false
		}
		return fmt.Sprintf("%s = %s\n", name, renderPrimitive(node.Primitive)), nil, nil, nil, true
	case ir.KindArray, ir.KindMap:
		res, extra := r.resolveType(node, name+"Item")
		if res.Skip {
			return unsupportedShapeComment(name), nil, nil, nil, false
		}
		return fmt.Sprintf("%s = %s\n", name, res.Expr), res.Deps, res.Imports, extra, true
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName, map[string]bool{}) {
			return unsupportedShapeComment(name), nil, nil, nil, false
		}
		target := r.nameFor(node.RefName)
		return fmt.Sprintf("%s = %s\n", name, target), []string{target}, nil, nil, true
	default:
		return "", nil, nil, nil, false
	}
}

// Generate renders every model in doc as a Python declaration for style,
// plus any declarations synthesized along the way for inline
// enums/objects. Order is deterministic: doc.Models first (name-sorted,
// as ir.Build produces them), then synthesized declarations in the order
// they were discovered — same contract as
// internal/gen/swift/generate.go's Generate.
func Generate(doc *ir.Document, style Style) ([]ModelOutput, error) {
	names := mobile.AssignNames(doc.Models, SanitizeTypeIdentifier)
	used := make(map[string]bool, len(names))
	for _, n := range names {
		used[n] = true
	}
	modelsByName := make(map[string]*ir.Model, len(doc.Models))
	for _, m := range doc.Models {
		modelsByName[m.Name] = m
	}
	r := &renderer{names: names, modelsByName: modelsByName, used: used, style: style}

	queue := make([]extraDecl, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, extraDecl{Name: names[m.Name], Node: m.Type})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, deps, imports, extra, alias := r.renderDeclaration(item.Name, item.Node)
		if decl == "" {
			continue
		}
		outputs = append(outputs, ModelOutput{
			Name:         item.Name,
			Declaration:  decl,
			Dependencies: removeName(dedupeSorted(deps), item.Name),
			Imports:      dedupeSorted(imports),
			Alias:        alias,
			Unsupported:  decl == unsupportedShapeComment(item.Name),
		})
		queue = append(queue, extra...)
	}
	return outputs, nil
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
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
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
