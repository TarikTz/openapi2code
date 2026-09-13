package ir

// Kind identifies which shape an IR Node has.
type Kind int

const (
	KindPrimitive Kind = iota
	KindObject
	KindArray
	KindEnum
	KindUnion
	KindIntersection
	KindRef
	KindMap
)

// Primitive identifies a scalar JSON Schema type.
type Primitive int

const (
	PrimitiveString Primitive = iota
	PrimitiveNumber
	PrimitiveInteger
	PrimitiveBoolean
	PrimitiveUnknown
)

// Node is a single type-shaped node in the IR. Exactly one of Object,
// Array, Enum, Union, Intersection, Map, or RefName is meaningful,
// selected by Kind; Primitive is meaningful only when Kind ==
// KindPrimitive.
type Node struct {
	Kind         Kind
	Primitive    Primitive
	Object       *ObjectNode
	Array        *ArrayNode
	Enum         *EnumNode
	Union        *UnionNode
	Intersection *IntersectionNode
	Map          *MapNode
	RefName      string
}

// Field is a named, typed member of an ObjectNode.
type Field struct {
	Name     string
	Type     *Node
	Optional bool
	Nullable bool
}

// ObjectNode is a struct-shaped type. Extends names other top-level
// models this object should be rendered as extending (populated when the
// object came from an allOf composed entirely of object-shaped members).
type ObjectNode struct {
	Extends []string
	Fields  []*Field
}

// ArrayNode is a homogeneous list type.
type ArrayNode struct {
	Items *Node
}

// MapNode is a string-keyed dictionary type, from a schema whose only
// content is `additionalProperties: <schema>` (JSON Schema's
// "additionalProperties: true", with no schema, and a schema that also
// declares its own Properties are both left as an ordinary object — see
// internal/ir/build.go's isMapShaped for the exact scoping).
type MapNode struct {
	Values *Node
}

// EnumNode is a closed set of literal string values.
type EnumNode struct {
	Values []string
}

// UnionNode is a set of alternative types (from oneOf/anyOf).
type UnionNode struct {
	Variants []*Node
}

// IntersectionNode is a set of types that must all be satisfied at once —
// the fallback rendering for an allOf that isn't entirely object-shaped.
type IntersectionNode struct {
	Members []*Node
}

// Model is one top-level named schema.
type Model struct {
	Name   string
	Type   *Node
	Cyclic bool
}

// Document is the fully-built IR for a parsed spec: every top-level
// schema, in deterministic (name-sorted) order.
type Document struct {
	Models []*Model
}
