// internal/gen/python/generate.go
package python

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// Style selects which Python flavor Generate renders: plain stdlib
// dataclasses, or Pydantic BaseModel subclasses. The two share every
// type-mapping, naming, and declaration-ordering rule below, differing
// only in class header, field default/alias syntax, and imports (see
// renderObjectDecl in generate_object.go, added by Task 4).
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
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
	Imports      []string
}

type extraDecl = mobile.PendingDecl

// typeResult is resolveType's return: a Python type expression plus
// everything a caller building a ModelOutput needs to aggregate across
// every field of an object (see resolveFields in generate_object.go).
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
	return fmt.Sprintf("# %s was not generated for the Python target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", name)
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
// resolveType's and renderDeclaration's bypass, added in Task 4).
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
