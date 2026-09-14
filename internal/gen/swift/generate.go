package swift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Swift declaration: a struct, a
// class (for cyclic models), an enum, or a typealias.
type ModelOutput struct {
	Name        string
	Declaration string
}

// extraDecl is a top-level declaration synthesized while resolving a
// field's type — an inline (non-$ref) enum or object needs a real
// top-level name in Swift, since Swift has no anonymous type literal for
// either shape. See the design spec's "Addendum: inline (field-level)
// enums" and its generalization to inline objects.
type extraDecl = mobile.PendingDecl

// Generate renders every model in doc as a Swift declaration, plus any
// declarations synthesized along the way for inline enums/objects. Order
// is deterministic: doc.Models first (name-sorted, as ir.Build produces
// them), then synthesized declarations in the order they were
// discovered.
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	names := mobile.AssignNames(doc.Models, SanitizeTypeIdentifier)
	used := make(map[string]bool, len(doc.Models))
	for _, n := range names {
		used[n] = true
	}
	modelsByName := make(map[string]*ir.Model, len(doc.Models))
	for _, m := range doc.Models {
		modelsByName[m.Name] = m
	}
	r := &renderer{names: names, modelsByName: modelsByName, used: used}

	queue := make([]extraDecl, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, extraDecl{Name: names[m.Name], Node: modelNode(m)})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, extra, err := r.renderDeclaration(item.Name, item.Node, isCyclicRoot(doc, item.Name, names))
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.Name, err)
		}
		if decl == "" {
			continue // defensive only: every reachable node.Kind now renders
			// something (a real declaration or an explanatory comment), so
			// this is unreachable in practice.
		}
		outputs = append(outputs, ModelOutput{Name: item.Name, Declaration: decl})
		// Each e's Name is already final and globally unique: resolveType's
		// ir.KindEnum/ir.KindObject case mints it via r.used at the moment
		// it synthesizes the extraDecl, and that same name is what the
		// field's type expression already embeds. Minting (and
		// uniquifying) again here would let the two disagree — see
		// resolveType's doc comment.
		queue = append(queue, extra...)
	}
	return outputs, nil
}

// modelNode returns m's own type node — a small indirection so Generate's
// queue can carry ir.Model-derived and synthesized items uniformly.
func modelNode(m *ir.Model) *ir.Node {
	return m.Type
}

// isCyclicRoot reports whether name (already resolved to its Swift
// identifier) belongs to a model flagged Cyclic in doc — only a
// document's own top-level models can be cyclic (a synthesized inline
// enum/object can never participate in a $ref cycle, since nothing can
// $ref an anonymous type), so this only needs to check doc.Models.
func isCyclicRoot(doc *ir.Document, name string, names map[string]string) bool {
	for _, m := range doc.Models {
		if names[m.Name] == name {
			return m.Cyclic
		}
	}
	return false
}

type renderer struct {
	names map[string]string

	// modelsByName looks up a document's top-level models by their
	// original IR name (as stored in ir.ObjectNode.Extends), so
	// flattenFields can resolve an allOf-derived Extends chain into the
	// base's actual fields. Swift has no notion of struct inheritance
	// here (per the design's "allOf always flattens" rule, unlike
	// internal/gen/ts's `extends` clause or internal/gen/zod's
	// `.extend()` chaining), so an Extends target's fields must be
	// copied in directly rather than referenced.
	modelsByName map[string]*ir.Model

	// used tracks every Swift type identifier minted so far: every
	// top-level model's assigned name (seeded by Generate before any
	// rendering starts) plus every synthesized inline enum/object name.
	// resolveType consults and updates this directly, at the exact
	// moment it mints a new synthesized name, so that name is decided
	// exactly once — see resolveType's doc comment for why computing it
	// twice (once here, once later) is the bug this field exists to
	// prevent.
	used map[string]bool
}

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

// renderDeclaration renders node as a top-level Swift declaration named
// name. cyclic is only meaningful when node.Kind == ir.KindObject (an
// enum or typealias is never cyclic; a synthesized inline object is never
// the target of a $ref, so it is never cyclic either — cyclic applies
// only to a document's own top-level object models).
func (r *renderer) renderDeclaration(name string, node *ir.Node, cyclic bool) (string, []extraDecl, error) {
	switch node.Kind {
	case ir.KindObject:
		return r.renderObjectDecl(name, node.Object, cyclic)
	case ir.KindEnum:
		// A top-level boolean enum has no valid Swift raw-value
		// representation (see swiftEnumBacking) — alias it to Bool
		// directly, mirroring the ir.KindPrimitive case just below
		// rather than emitting a broken `enum X: Bool`.
		if node.Enum.Primitive == ir.PrimitiveBoolean {
			return fmt.Sprintf("public typealias %s = Bool\n", name), nil, nil
		}
		return r.renderEnumDecl(name, node.Enum), nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return unsupportedShapeComment(name), nil, nil // per the design's unsupported-shape rule
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, renderPrimitive(node.Primitive)), nil, nil
	case ir.KindArray:
		// name+"Item" (not name) seeds the synthesized name an inline
		// enum/object item would get: this model's own type IS the
		// array, so passing name itself here would make a top-level
		// `Tags: [<inline object>]` schema synthesize its item type as
		// "Tags" too — producing the self-referential, non-compiling
		// `public typealias Tags = [Tags]`.
		expr, skip, extra := r.resolveType(node, name+"Item")
		if skip {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, expr), extra, nil
	case ir.KindMap:
		// Same name+"Item" seeding as KindArray above, for the same
		// self-reference reason (this model's own type IS the map).
		expr, skip, extra := r.resolveType(node, name+"Item")
		if skip {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, expr), extra, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName, map[string]bool{}) {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("public typealias %s = %s\n", name, r.nameFor(node.RefName)), nil, nil
	default:
		return "", nil, nil
	}
}

// refIsUnsupported reports whether the model named modelName would
// render as an unsupportedShapeComment (see renderDeclaration) rather
// than a real declaration. It's purely a classification — unlike
// resolveType, it never mints a synthesized name or otherwise mutates
// renderer state, so it's safe to call from every field/typealias that
// $refs the same target without creating duplicate synthesized
// declarations. It follows a pure-$ref alias chain, guarded against a
// cyclic chain via visited.
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

// nodeIsUnsupported is refIsUnsupported's node-level counterpart, for a
// node reached mid-walk (e.g. an array's item type) rather than a named
// model. It mirrors resolveType's skip conditions exactly, without any
// of resolveType's name-synthesizing side effects.
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
// place of name's real declaration when its shape has no clean Swift
// representation (oneOf/anyOf, an allOf mixing object and non-object
// members, or an unrecognized primitive type) — see the design spec's
// unsupported-shape rule. Generation continues for the rest of the
// document; only this one declaration is affected.
func unsupportedShapeComment(name string) string {
	return fmt.Sprintf("// %s was not generated for the Swift target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", name)
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "String"
	case ir.PrimitiveNumber:
		return "Double"
	case ir.PrimitiveInteger:
		return "Int"
	case ir.PrimitiveBoolean:
		return "Bool"
	default:
		return "String"
	}
}

// renderEnumDecl renders e as a String-raw-valued enum. Case identifiers
// are uniquified within this one enum (used is local to this call): two
// DIFFERENT wire values can converge on the same identifier under
// ToCamelCase (e.g. "photo_urls" and "photoUrls" both becoming
// "photoUrls"), which would otherwise emit two identically-named cases —
// a compile error. The raw value written after `=` is always the case's
// OWN original wire string, so the disambiguating suffix never changes
// what a value decodes from or encodes to.
func (r *renderer) renderEnumDecl(name string, e *ir.EnumNode) string {
	rawType, formatLiteral := swiftEnumBacking(e.Primitive)
	var sb strings.Builder
	fmt.Fprintf(&sb, "public enum %s: %s, Codable {\n", name, rawType)
	used := make(map[string]bool, len(e.Values))
	for _, v := range e.Values {
		caseName := mobile.Uniquify(SanitizeEnumCaseIdentifier(v), used)
		used[caseName] = true
		if e.Primitive == ir.PrimitiveString && caseName == v {
			fmt.Fprintf(&sb, "    case %s\n", caseName)
		} else {
			fmt.Fprintf(&sb, "    case %s = %s\n", caseName, formatLiteral(v))
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// swiftEnumBacking returns the Swift raw-value type backing an enum for
// p, plus a function rendering one enum value as that type's literal
// syntax. Only String/Int/Double are valid Swift enum raw-value types —
// PrimitiveBoolean never reaches here: Swift's compiler-synthesized
// RawRepresentable conformance does not support Bool as a raw type at
// all, so a boolean enum is bypassed entirely in favor of a plain Bool
// field/typealias before renderEnumDecl or this function is ever
// consulted (see resolveType's ir.KindEnum case and renderDeclaration's
// same case).
func swiftEnumBacking(p ir.Primitive) (swiftType string, formatLiteral func(v string) string) {
	switch p {
	case ir.PrimitiveInteger, ir.PrimitiveNumber:
		return renderPrimitive(p), func(v string) string { return v }
	default:
		return "String", func(v string) string { return strconv.Quote(v) }
	}
}

// resolvedField is one field ready to render into a struct/class body.
type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	kind         ir.Kind // the field's own node kind, needed by decodeExpr/encodeExpr below
	optional     bool
}

// resolveFields resolves every field of an object into resolvedFields,
// synthesizing a top-level name for any inline enum/object it encounters.
// containingTypeName seeds the synthesized name (<ModelName><FieldName>).
// skipped carries the wire (original JSON) name of every field omitted
// because resolveType found no representable Swift type for it — per the
// design's unsupported-shape rule, the caller surfaces these as comments
// rather than dropping them without a trace.
//
// Property identifiers are uniquified within this one object (used is
// local to this call, so nothing leaks across objects): two DIFFERENT
// wire names can converge on the same identifier under ToCamelCase (e.g.
// "photo_urls" and "photoUrls" both becoming "photoUrls"), which would
// otherwise emit two identically-named properties — a compile error.
// This runs on the ALREADY-FLATTENED field list, so it also covers an
// inherited-via-allOf field colliding with an own field this way.
// flattenFields handles the different concern of two fields sharing an
// IDENTICAL wire name. wireName always stays the field's own original
// name, so CodingKeys still maps each property to the right JSON key.
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) (resolved []resolvedField, allExtra []extraDecl, skipped []string) {
	used := make(map[string]bool, len(fields))
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		expr, skip, extra := r.resolveType(f.Type, suggested)
		if skip {
			skipped = append(skipped, f.Name)
			continue
		}
		optional := f.Optional || f.Nullable
		if optional {
			expr += "?"
		}
		propertyName := mobile.Uniquify(SanitizeFieldIdentifier(f.Name), used)
		used[propertyName] = true
		resolved = append(resolved, resolvedField{
			propertyName: propertyName,
			wireName:     f.Name,
			typeExpr:     expr,
			kind:         f.Type.Kind,
			optional:     optional,
		})
		allExtra = append(allExtra, extra...)
	}
	return resolved, allExtra, skipped
}

// resolveType resolves node to a non-optional Swift type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object. skip is true when node has no
// representable Swift type (a union, an allOf-with-non-object-member
// intersection, or an unrecognized primitive) — the caller omits
// whatever position this type would have occupied (see
// unsupportedShapeComment for how that's surfaced instead of silently
// dropped).
//
// For the ir.KindEnum/ir.KindObject case, the name a synthesized
// declaration is finally enqueued under (in Generate's queue) must be
// exactly the name embedded in the type expression returned here — they
// are read by different code at different times, so if this function
// returned suggestedName as-is and left uniquification for Generate to
// do later, the two could disagree whenever suggestedName collides with
// an existing name (an already-used synthesized name, or worse, an
// unrelated top-level model's actual name) — the type expression would
// point at the WRONG existing declaration while the real synthesized one
// gets stranded under a `_2`-suffixed name nothing references. Minting
// the final, unique name here — once, via r.used — and returning that
// same name as both the expression and the extraDecl's name closes that
// gap.
func (r *renderer) resolveType(node *ir.Node, suggestedName string) (expr string, skip bool, extra []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", true, nil
		}
		return renderPrimitive(node.Primitive), false, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName, map[string]bool{}) {
			return "", true, nil
		}
		return r.nameFor(node.RefName), false, nil
	case ir.KindArray:
		itemExpr, itemSkip, itemExtra := r.resolveType(node.Array.Items, suggestedName)
		if itemSkip {
			return "", true, nil
		}
		return "[" + itemExpr + "]", false, itemExtra
	case ir.KindMap:
		// Swift's Dictionary is Codable for free whenever its Value type
		// is — no hand-written serialization needed, unlike Kotlin/Dart.
		valuesExpr, valuesSkip, valuesExtra := r.resolveType(node.Map.Values, suggestedName)
		if valuesSkip {
			return "", true, nil
		}
		return "[String: " + valuesExpr + "]", false, valuesExtra
	case ir.KindEnum:
		// Bool can't back a Swift enum's raw value at all (unlike Int,
		// Double, and String — see swiftEnumBacking's doc comment), so a
		// boolean enum bypasses synthesizing a named declaration
		// entirely and resolves straight to Bool, same as an ordinary
		// boolean field.
		if node.Enum.Primitive == ir.PrimitiveBoolean {
			return "Bool", false, nil
		}
		name := mobile.Uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		return name, false, []extraDecl{{Name: name, Node: node}}
	case ir.KindObject:
		name := mobile.Uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		return name, false, []extraDecl{{Name: name, Node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return "", true, nil
	default:
		return "", true, nil
	}
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the design's "allOf always flattens" rule — Swift
// structs model no inheritance here, unlike ts's `extends` clause or
// zod's `.extend()` chaining, so a base's fields must be copied in
// directly. Extends targets are walked first (in order, base-fields-first
// for output readability), then o's own fields — but o's own fields take
// precedence on a name collision: a field the object redeclares itself
// overrides the same-named field inherited from a base, matching
// internal/gen/ts's `extends` (the object's own field literally shadows
// the base's) and internal/gen/zod's `.extend()` (whose own-fields object
// is merged in last, overriding the base). A base's field is skipped
// outright — rather than de-duplicated after the fact — whenever the
// object itself already declares that name, so the returned list never
// contains both.
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
	// o.Fields is appended unconditionally, not through addBase's
	// dedup — ownNames above already guarantees nothing already in
	// fields shares a name with it.
	return append(fields, o.Fields...)
}

// flattenExtendsTarget returns modelName's full field list (recursively
// flattening its own Extends, if any), applying the same own-field-wins
// precedence flattenFields applies at the outermost level to every
// intermediate level too — otherwise a 3+-level allOf chain lets a
// grandchild silently revert to a grandparent's field type instead of
// the parent's override (the two would then disagree within one
// generated file on what is logically the same field).
//
// modelName is first followed through a pure-$ref alias chain down to
// the underlying object, if any: ir.Build collapses a single-ref,
// no-own-properties allOf (the common "wrap a $ref in allOf just to
// attach a description" idiom) into a bare KindRef, so an Extends target
// with Kind == KindRef still has real fields worth inheriting, one hop
// away.
//
// visited guards against a malformed/cyclic Extends or alias graph
// turning into infinite recursion; a well-formed document never needs
// it; see internal/gen/zod's computeExtendable doc comment for why the
// Extends-only graph is expected to be acyclic in practice.
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

// renderObjectDecl renders o as a struct (or, when cyclic, a class with a
// hand-written init(from:) — Codable still synthesizes encode(to:) for a
// class with a custom init(from:), confirmed against the real Swift
// compiler). o.Fields alone is not yet
// flattened for an allOf whose members include a $ref (see
// flattenFields), so renderObjectDecl resolves the full field list
// through flattenFields rather than reading o.Fields directly.
//
// When every field was skipped (or there were none to begin with), no
// CodingKeys enum is emitted: an empty raw-value enum is a compile error
// in Swift ("an enum with no cases cannot declare a raw type"), and it
// serves no purpose here anyway — Swift's default Codable synthesis
// handles a type with zero stored properties on its own. The cyclic
// (class) branch mirrors this with a parameterless init() and an
// init(from:) that reads nothing from the decoder.
func (r *renderer) renderObjectDecl(name string, o *ir.ObjectNode, cyclic bool) (string, []extraDecl, error) {
	fields, extra, skipped := r.resolveFields(name, r.flattenFields(o))

	var sb strings.Builder
	kind := "struct"
	if cyclic {
		kind = "class"
	}
	fmt.Fprintf(&sb, "public %s %s: Codable {\n", kind, name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "    public var %s: %s\n", f.propertyName, f.typeExpr)
	}
	for _, wireName := range skipped {
		fmt.Fprintf(&sb, "    // %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", wireName)
	}

	if len(fields) > 0 {
		sb.WriteString("\n    enum CodingKeys: String, CodingKey {\n")
		for _, f := range fields {
			fmt.Fprintf(&sb, "        case %s = %s\n", f.propertyName, strconv.Quote(f.wireName))
		}
		sb.WriteString("    }\n")
	}

	if cyclic {
		if len(fields) == 0 {
			sb.WriteString("\n    public init() {}\n")
			sb.WriteString("\n    public required init(from decoder: Decoder) throws {}\n")
		} else {
			sb.WriteString("\n    public init(")
			for i, f := range fields {
				if i > 0 {
					sb.WriteString(", ")
				}
				fmt.Fprintf(&sb, "%s: %s", f.propertyName, f.typeExpr)
			}
			sb.WriteString(") {\n")
			for _, f := range fields {
				fmt.Fprintf(&sb, "        self.%s = %s\n", f.propertyName, f.propertyName)
			}
			sb.WriteString("    }\n")

			sb.WriteString("\n    public required init(from decoder: Decoder) throws {\n")
			sb.WriteString("        let container = try decoder.container(keyedBy: CodingKeys.self)\n")
			for _, f := range fields {
				base := strings.TrimSuffix(f.typeExpr, "?")
				if f.optional {
					fmt.Fprintf(&sb, "        self.%s = try container.decodeIfPresent(%s.self, forKey: .%s)\n", f.propertyName, base, f.propertyName)
				} else {
					fmt.Fprintf(&sb, "        self.%s = try container.decode(%s.self, forKey: .%s)\n", f.propertyName, base, f.propertyName)
				}
			}
			sb.WriteString("    }\n")
		}
	}

	sb.WriteString("}\n")
	return sb.String(), extra, nil
}
