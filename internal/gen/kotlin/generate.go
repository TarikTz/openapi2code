package kotlin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Kotlin declaration: a data
// class, an enum class, or a typealias.
type ModelOutput struct {
	Name        string
	Declaration string
}

type extraDecl = mobile.PendingDecl

// modelKind classifies a $ref target so fromJson/toJson know which
// expression shape to generate for it (a nested ref could point at
// either an object or an enum, which need different serialization code).
type modelKind int

const (
	modelKindObject modelKind = iota
	modelKindEnum
	modelKindOther
)

// Generate renders every model in doc as a Kotlin declaration, plus any
// declarations synthesized for inline enums/objects, mirroring
// internal/gen/swift's Generate exactly in structure (see that package's
// comments for the full rationale — this is an independent
// implementation of the same design, not a shared one, since Kotlin's
// syntax and serialization approach differ enough that sharing code would
// cost more in indirection than it saves).
func Generate(doc *ir.Document) ([]ModelOutput, error) {
	names := mobile.AssignNames(doc.Models, SanitizeTypeIdentifier)
	used := make(map[string]bool, len(doc.Models))
	for _, n := range names {
		used[n] = true
	}
	kinds := make(map[string]modelKind, len(doc.Models))
	for _, m := range doc.Models {
		kinds[names[m.Name]] = kindOf(m.Type)
	}
	modelsByName := make(map[string]*ir.Model, len(doc.Models))
	for _, m := range doc.Models {
		modelsByName[m.Name] = m
	}
	r := &renderer{names: names, kinds: kinds, modelsByName: modelsByName, used: used}

	queue := make([]extraDecl, 0, len(doc.Models))
	for _, m := range doc.Models {
		queue = append(queue, extraDecl{Name: names[m.Name], Node: m.Type})
	}

	var outputs []ModelOutput
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		decl, extra, err := r.renderDeclaration(item.Name, item.Node)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.Name, err)
		}
		if decl == "" {
			continue
		}
		outputs = append(outputs, ModelOutput{Name: item.Name, Declaration: decl})
		// Each e's Name is already final and globally unique, and
		// r.kinds[e.Name] is already populated: resolveType mints the
		// name and records its kind in r.used/r.kinds at the exact
		// moment it synthesizes the extraDecl (see resolveType's doc
		// comment). Re-deriving either here — as an earlier version of
		// this function did — would run too late: toJsonExpr/
		// fromJsonExpr for the declaration just rendered above may
		// already have consulted r.kinds for this very name (e.g. Pet
		// referencing its own inline PetStatus enum while rendering
		// Pet's toJson()), and would have seen a missing entry.
		queue = append(queue, extra...)
	}
	return outputs, nil
}

func kindOf(node *ir.Node) modelKind {
	switch node.Kind {
	case ir.KindObject:
		return modelKindObject
	case ir.KindEnum:
		return modelKindEnum
	default:
		return modelKindOther
	}
}

// modelsByName looks up a document's top-level models by their IR name
// (not their resolved Kotlin identifier) — so flattenFields can resolve
// an allOf-derived Extends chain into the actual base model's Fields.
type renderer struct {
	names        map[string]string
	kinds        map[string]modelKind
	modelsByName map[string]*ir.Model

	// used tracks every Kotlin type identifier minted so far: every
	// top-level model's assigned name (seeded by Generate before any
	// rendering starts) plus every synthesized inline enum/object name.
	// resolveType consults and updates this directly, at the exact moment
	// it mints a new synthesized name, so that name — and its r.kinds
	// entry — are decided exactly once. See resolveType's doc comment for
	// why minting (or classifying) it a second time elsewhere would let
	// the type expression, the extraDecl, and the field's dispatch node
	// disagree.
	used map[string]bool
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the design's "allOf always flattens" rule — Kotlin
// data classes model no inheritance here (data classes cannot extend
// each other), so a base's fields must be copied in directly. o.Fields
// alone is NOT already flattened: internal/ir's buildAllOfNode records a
// $ref allOf member only in Extends, never in Fields — only inline
// object members land in Fields. Extends targets are walked first (in
// order, base-fields-first for output readability), then o's own fields —
// but o's own fields take precedence on a name collision: a field the
// object redeclares itself overrides the same-named field inherited from
// a base, matching internal/gen/ts's `extends` (the object's own field
// literally shadows the base's) and internal/gen/zod's `.extend()` (whose
// own-fields object is merged in last, overriding the base). A base's
// field is skipped outright — rather than de-duplicated after the fact —
// whenever the object itself already declares that name, so the returned
// list never contains both.
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
	// o.Fields is appended unconditionally, not through addBase's dedup —
	// ownNames above already guarantees nothing already in fields shares a
	// name with it.
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
// turning into infinite recursion.
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

func (r *renderer) nameFor(modelName string) string {
	if n, ok := r.names[modelName]; ok {
		return n
	}
	return SanitizeTypeIdentifier(modelName)
}

func (r *renderer) renderDeclaration(name string, node *ir.Node) (string, []extraDecl, error) {
	switch node.Kind {
	case ir.KindObject:
		return r.renderDataClass(name, node.Object)
	case ir.KindEnum:
		return r.renderEnumClass(name, node.Enum), nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return unsupportedShapeComment(name), nil, nil // per the design's unsupported-shape rule
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, renderPrimitive(node.Primitive)), nil, nil
	case ir.KindArray:
		// name+"Item" (not name) seeds the synthesized name an inline
		// enum/object item would get: this model's own type IS the array, so
		// passing name itself here would make a top-level `Tags: [<inline
		// object>]` schema try to synthesize its item type as "Tags" too —
		// saved from a literal self-reference only incidentally, by
		// uniquify's numeric-suffix fallback ("Tags_2"). internal/gen/swift
		// uses the same "<Name>Item" convention, and the design spec
		// requires the synthesized-name convention to be identical across
		// all three languages.
		expr, _, skip, extra := r.resolveType(node, name+"Item")
		if skip {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, expr), extra, nil
	case ir.KindMap:
		// Same name+"Item" seeding as KindArray above, for the same
		// self-reference reason (this model's own type IS the map).
		expr, _, skip, extra := r.resolveType(node, name+"Item")
		if skip {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, expr), extra, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName) {
			return unsupportedShapeComment(name), nil, nil
		}
		return fmt.Sprintf("typealias %s = %s\n", name, r.nameFor(node.RefName)), nil, nil
	default:
		return "", nil, nil
	}
}

// refIsUnsupported reports whether refName would render as an
// unsupportedShapeComment (see renderDeclaration) rather than a real
// declaration. A ref straight to a real object/enum model is never
// unsupported. Anything else (a modelKindOther target) is resolved
// through the alias chain via resolveAliasTarget — the same walk
// resolveRefDispatchNode already performs for serialization dispatch —
// and classified by nodeIsUnsupported. It's purely a classification, with
// none of resolveType's name-synthesizing side effects, so it's safe to
// call from every field/typealias that $refs the same target without
// creating duplicate synthesized declarations.
func (r *renderer) refIsUnsupported(refName string) bool {
	switch r.refKind(refName) {
	case modelKindEnum, modelKindObject:
		return false
	default:
		resolved, _, ok := r.resolveAliasTarget(refName, map[string]bool{})
		if !ok {
			return true
		}
		return r.nodeIsUnsupported(resolved)
	}
}

// nodeIsUnsupported mirrors resolveType's/renderDeclaration's skip
// conditions for a node that isn't (or is no longer, after alias
// resolution) a bare $ref.
func (r *renderer) nodeIsUnsupported(node *ir.Node) bool {
	switch node.Kind {
	case ir.KindRef:
		return r.refIsUnsupported(node.RefName)
	case ir.KindPrimitive:
		return node.Primitive == ir.PrimitiveUnknown
	case ir.KindArray:
		return r.nodeIsUnsupported(node.Array.Items)
	case ir.KindMap:
		return r.nodeIsUnsupported(node.Map.Values)
	case ir.KindUnion, ir.KindIntersection:
		return true
	default: // KindEnum, KindObject: resolveType always synthesizes a real declaration for these
		return false
	}
}

// unsupportedShapeComment renders the comment-only declaration emitted in
// place of name's real declaration when its shape has no clean Kotlin
// representation (oneOf/anyOf, an allOf mixing object and non-object
// members, or an unrecognized primitive type) — see the design spec's
// unsupported-shape rule. Generation continues for the rest of the
// document; only this one declaration is affected.
func unsupportedShapeComment(name string) string {
	return fmt.Sprintf("// %s was not generated for the Kotlin target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", name)
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
		return "Boolean"
	default:
		return "String"
	}
}

// renderEnumClass renders e as an enum class carrying its wire string in
// `value`. Constant identifiers are uniquified within this one enum (used
// is local to this call): two DIFFERENT wire values can converge on the
// same identifier under ToScreamingSnakeCase (e.g. "photo_urls" and
// "photoUrls" both becoming "PHOTO_URLS"), which would otherwise emit two
// identically-named constants — a compile error. The constructor argument
// is always the constant's OWN original wire string, so the
// disambiguating suffix never changes what fromValue()/`.value` see.
func (r *renderer) renderEnumClass(name string, e *ir.EnumNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "enum class %s(val value: String) {\n", name)
	used := make(map[string]bool, len(e.Values))
	for i, v := range e.Values {
		constName := mobile.Uniquify(SanitizeEnumConstantIdentifier(v), used)
		used[constName] = true
		comma := ","
		if i == len(e.Values)-1 {
			comma = ";"
		}
		fmt.Fprintf(&sb, "    %s(%s)%s\n", constName, strconv.Quote(v), comma)
	}
	sb.WriteString("\n    companion object {\n")
	fmt.Fprintf(&sb, "        fun fromValue(value: String): %s = values().first { it.value == value }\n", name)
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String()
}

type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	node         *ir.Node // the field's own (non-optional-wrapped) resolved node, for fromJson/toJson codegen
	optional     bool
}

// resolveFields resolves every field of an object into resolvedFields,
// synthesizing a top-level name for any inline enum/object it encounters.
// containingTypeName seeds the synthesized name (<ModelName><FieldName>).
// skipped carries the wire (original JSON) name of every field omitted
// because resolveType found no representable Kotlin type for it — per the
// design's unsupported-shape rule, the caller surfaces these as comments
// rather than dropping them without a trace.
//
// Property identifiers are uniquified within this one object (used is
// local to this call, so nothing leaks across objects): two DIFFERENT
// wire names can converge on the same identifier under ToCamelCase (e.g.
// "photo_urls" and "photoUrls" both becoming "photoUrls"), which would
// otherwise emit two identically-named constructor parameters — a compile
// error. This runs on the ALREADY-FLATTENED field list, so it also covers
// an inherited-via-allOf field colliding with an own field this way.
// flattenFields handles the different concern of two fields sharing an
// IDENTICAL wire name. wireName always stays the field's own original
// name, so toJson()/fromJson() still read and write the right JSON key.
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) (resolved []resolvedField, allExtra []extraDecl, skipped []string) {
	used := make(map[string]bool, len(fields))
	for _, f := range fields {
		suggested := containingTypeName + mobile.ToPascalCase(f.Name)
		expr, effective, skip, extra := r.resolveType(f.Type, suggested)
		if skip {
			skipped = append(skipped, f.Name)
			continue
		}
		optional := f.Optional || f.Nullable
		typeExpr := expr
		if optional {
			typeExpr += "?"
		}
		propertyName := mobile.Uniquify(SanitizeFieldIdentifier(f.Name), used)
		used[propertyName] = true
		resolved = append(resolved, resolvedField{
			propertyName: propertyName,
			wireName:     f.Name,
			typeExpr:     typeExpr,
			node:         effective,
			optional:     optional,
		})
		allExtra = append(allExtra, extra...)
	}
	return resolved, allExtra, skipped
}

// resolveType resolves node to a non-optional Kotlin type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object. skip is true when node has no
// representable Kotlin type (a union, an allOf-with-non-object-member
// intersection, or an unrecognized primitive) — the caller omits
// whatever position this type would have occupied.
//
// effective is the node fromJson/toJson codegen (toJsonExpr/fromJsonExpr)
// should switch on for this field: for a real $ref it's node itself; for
// an inline enum/object it's a synthetic ir.KindRef pointing at the exact
// name minted just below, so the same ref-handling code path covers both
// a real $ref and a synthesized one uniformly; for an array it recurses
// per element.
//
// For the ir.KindEnum/ir.KindObject case, the name a synthesized
// declaration is finally enqueued under (in Generate's queue) — and the
// modelKind recorded for it in r.kinds — must be exactly the name
// embedded in both the returned expr and the returned effective node:
// they are read by different code at different times (expr appears in
// this field's type declaration; effective is consulted later, by
// toJsonExpr/fromJsonExpr on this very same field, and again by Generate
// once this declaration's rendering has finished and it enqueues extra).
// Minting the final, unique name here — once, via r.used — and recording
// its kind in r.kinds in the same step, closes two gaps at once: a name
// collision can no longer make the type expression point at the wrong
// declaration, and toJsonExpr/fromJsonExpr can no longer observe a
// not-yet-populated r.kinds entry for a name this very call is
// synthesizing.
func (r *renderer) resolveType(node *ir.Node, suggestedName string) (expr string, effective *ir.Node, skip bool, extra []extraDecl) {
	switch node.Kind {
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return "", nil, true, nil
		}
		return renderPrimitive(node.Primitive), node, false, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName) {
			return "", nil, true, nil
		}
		return r.nameFor(node.RefName), r.effectiveRefNode(node), false, nil
	case ir.KindArray:
		itemExpr, itemEffective, itemSkip, itemExtra := r.resolveType(node.Array.Items, suggestedName)
		if itemSkip {
			return "", nil, true, nil
		}
		effective := &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: itemEffective}}
		return "List<" + itemExpr + ">", effective, false, itemExtra
	case ir.KindMap:
		valuesExpr, valuesEffective, valuesSkip, valuesExtra := r.resolveType(node.Map.Values, suggestedName)
		if valuesSkip {
			return "", nil, true, nil
		}
		effective := &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: valuesEffective}}
		return "Map<String, " + valuesExpr + ">", effective, false, valuesExtra
	case ir.KindEnum, ir.KindObject:
		name := mobile.Uniquify(SanitizeTypeIdentifier(suggestedName), r.used)
		r.used[name] = true
		r.kinds[name] = kindOf(node)
		ref := &ir.Node{Kind: ir.KindRef, RefName: name}
		return name, ref, false, []extraDecl{{Name: name, Node: node}}
	case ir.KindUnion, ir.KindIntersection:
		return "", nil, true, nil
	default:
		return "", nil, true, nil
	}
}

// refKind reports the modelKind of the top-level model named refName —
// the raw IR schema name, exactly as stored on an ir.Node's RefName field.
// r.kinds itself is keyed by the resolved Kotlin identifier (see Generate
// and resolveType's ir.KindEnum/ir.KindObject case), so a raw name must
// always be routed through r.nameFor before it can be used to key into
// r.kinds. Looking r.kinds up directly by a raw schema name only agrees
// with this when that name happens to need no sanitization and never
// collided with anything — a schema literally named "Pet-Status", one
// needing a reserved-word suffix, or one that lost a name collision to a
// same-named sibling (picking up a "_2" suffix) would all silently
// mismatch and fall through to the wrong (object) dispatch branch.
func (r *renderer) refKind(refName string) modelKind {
	return r.kinds[r.nameFor(refName)]
}

// resolveAliasTarget walks refName's own top-level model definition
// through a chain of pure $ref aliases — a schema whose entire body is
// `$ref: ...`, which internal/ir's buildNode renders as a bare
// ir.Node{Kind: KindRef} at the top level just as readily as anywhere
// else — returning the first non-$ref node it finds, together with the
// model name it was actually declared under (which may differ from
// refName when the chain is more than one link long). visited guards
// against a malformed/cyclic alias chain; ok is false when refName isn't
// a known model, has no recorded type, or the chain is cyclic.
func (r *renderer) resolveAliasTarget(refName string, visited map[string]bool) (resolved *ir.Node, resolvedName string, ok bool) {
	if visited[refName] {
		return nil, "", false
	}
	visited[refName] = true
	m, found := r.modelsByName[refName]
	if !found || m.Type == nil {
		return nil, "", false
	}
	if m.Type.Kind == ir.KindRef {
		return r.resolveAliasTarget(m.Type.RefName, visited)
	}
	return m.Type, refName, true
}

// effectiveRefNode returns the node toJsonExpr/fromJsonExpr (and their
// item-level counterparts, itemToJsonExpr/itemFromJsonExpr) should
// dispatch on for a $ref to node.RefName. It's a thin entry point over
// resolveRefDispatchNode, which does the actual (recursive) work — see
// that function's doc comment for the full explanation. A single visited
// set is minted here and threaded through the whole resolution triggered
// by this one $ref, including any array-item resolution nested inside it
// (see resolveRefDispatchNode's ir.KindArray case), so a cyclic alias
// graph reached through an array's own item type — not just a straight
// alias-to-alias chain — cannot recurse forever.
func (r *renderer) effectiveRefNode(node *ir.Node) *ir.Node {
	return r.resolveRefDispatchNode(node, map[string]bool{})
}

// resolveRefDispatchNode resolves node for dispatch purposes: if node
// isn't a $ref at all, it's already concrete and is returned unchanged —
// this is what a KindArray's Items being an inline (non-$ref) object or
// enum falls through to (see the KindArray case below for why that's a
// known, disclosed gap rather than something this function fixes).
//
// For an actual $ref, a ref straight to a real top-level object or enum
// model already has everything toJsonExpr/fromJsonExpr need — r.kinds has
// a correct entry for the model's own name — so it's returned unchanged.
// Anything else is a modelKindOther target: a named array alias
// (`typealias Tags = List<Tag>`), a named primitive alias (`typealias
// PetId = String`), or a further $ref (alias-to-alias). Kotlin's
// typealias is structurally transparent — it IS the underlying type at
// compile time, not a distinct wrapper type — so a field of that type
// must be (de)serialized exactly as if it held the underlying concrete
// shape directly, not as an object with its own toJson()/fromJson(). This
// walks the alias chain via resolveAliasTarget to find that shape:
//
//   - a concrete primitive underneath is substituted in directly, so
//     toJsonExpr/fromJsonExpr's existing primitive branch takes over
//     unchanged;
//   - a concrete array underneath has its OWN Items node resolved too,
//     recursively, before being substituted in: resolveAliasTarget only
//     follows a top-level $ref chain, so without this an array alias
//     whose items are themselves a $ref to a further alias (e.g. `ItemId:
//     {type: string}`, `ItemIds: {type: array, items: {$ref: ItemId}}`)
//     would have its unresolved `{$ref: ItemId}` items node substituted
//     in verbatim — item(To|From)JsonExpr would then see a plain $ref to
//     a modelKindOther target and fall through to their own object-shaped
//     default, generating `ItemId.fromJson(...)`/`it.toJson()` for what
//     is actually a String. Recursing through this same function fixes
//     that. It does NOT fix the sibling case where the array alias's
//     items are an INLINE (non-$ref) object or enum — e.g. `Things:
//     {type: array, items: {type: object, ...}}` — because that item type
//     was synthesized under a fresh name (e.g. "Things_2") back when
//     Things was originally rendered as its own top-level declaration (in
//     a separate, earlier call to resolveType), and that name isn't
//     recoverable here: re-deriving it independently would risk the two
//     derivations disagreeing, exactly the bug class Critical Findings
//     1+2 from this generator's first review round were about. A $ref to
//     such an array alias therefore still declares the correct field type
//     (the alias name itself) but its toJson()/fromJson() will incorrectly
//     treat the inline item type as a plain value rather than calling its
//     (correctly-generated, just unreferenced from here) toJson()/
//     fromJson() — a known, disclosed gap, not silently wrong in a way
//     that also affects the item type's own declaration.
//   - a concrete object or enum reached through one or more aliases needs
//     its OWN name substituted in (not the alias's): fromJsonExpr and
//     itemFromJsonExpr call target.fromJson(...)/target.fromValue(...),
//     and only the real object/enum type has that companion object — a
//     typealias has none of its own to call those on, and
//     "PetIdAlias.fromJson(...)" would not compile even though
//     "petIdAlias.toJson()" (an instance call, resolved structurally)
//     would;
//   - a chain that bottoms out unresolvable (an unknown or cyclic
//     reference) or at a union/intersection — itself already an
//     unsupported shape rendered as a comment-only declaration with no
//     toJson/fromJson to call — falls back to node unchanged: the prior
//     (object-shaped) dispatch this fix does not attempt to make correct
//     for a case with no clean Kotlin representation either way.
func (r *renderer) resolveRefDispatchNode(node *ir.Node, visited map[string]bool) *ir.Node {
	if node.Kind != ir.KindRef {
		return node
	}
	switch r.refKind(node.RefName) {
	case modelKindEnum, modelKindObject:
		return node
	default:
		resolved, resolvedName, ok := r.resolveAliasTarget(node.RefName, visited)
		if !ok {
			return node
		}
		switch resolved.Kind {
		case ir.KindPrimitive:
			return resolved
		case ir.KindArray:
			items := r.resolveRefDispatchNode(resolved.Array.Items, visited)
			return &ir.Node{Kind: ir.KindArray, Array: &ir.ArrayNode{Items: items}}
		case ir.KindMap:
			values := r.resolveRefDispatchNode(resolved.Map.Values, visited)
			return &ir.Node{Kind: ir.KindMap, Map: &ir.MapNode{Values: values}}
		case ir.KindObject, ir.KindEnum:
			return &ir.Node{Kind: ir.KindRef, RefName: resolvedName}
		default:
			return node
		}
	}
}

// renderDataClass renders o as a data class. o.Fields alone is not yet
// flattened for an allOf whose members include a $ref (see
// flattenFields), so this resolves the full field list through
// flattenFields rather than reading o.Fields directly.
//
// When every field was skipped (or there were none to begin with), this
// does NOT emit `data class Name()`: Kotlin requires a data class's
// primary constructor to have at least one parameter ("Data class must
// have at least one primary constructor parameter" — a compile error), so
// a zero-field model falls back to a plain class with a parameterless
// constructor instead, which needs no primary-constructor parameter list
// at all.
func (r *renderer) renderDataClass(name string, o *ir.ObjectNode) (string, []extraDecl, error) {
	fields, extra, skipped := r.resolveFields(name, r.flattenFields(o))

	var sb strings.Builder
	if len(fields) == 0 {
		fmt.Fprintf(&sb, "class %s {\n", name)
		for _, wireName := range skipped {
			fmt.Fprintf(&sb, "    // %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", wireName)
		}
		if len(skipped) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("    fun toJson(): Map<String, Any?> {\n")
		sb.WriteString("        return emptyMap()\n")
		sb.WriteString("    }\n")
		sb.WriteString("\n    companion object {\n")
		fmt.Fprintf(&sb, "        fun fromJson(json: Map<String, Any?>): %s {\n", name)
		fmt.Fprintf(&sb, "            return %s()\n", name)
		sb.WriteString("        }\n")
		sb.WriteString("    }\n")
		sb.WriteString("}\n")
		return sb.String(), extra, nil
	}

	fmt.Fprintf(&sb, "data class %s(\n", name)
	for i, f := range fields {
		comma := ","
		if i == len(fields)-1 {
			comma = ""
		}
		def := ""
		if f.optional {
			def = " = null"
		}
		fmt.Fprintf(&sb, "    val %s: %s%s%s\n", f.propertyName, f.typeExpr, def, comma)
	}
	for _, wireName := range skipped {
		fmt.Fprintf(&sb, "    // %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", wireName)
	}
	sb.WriteString(") {\n")

	sb.WriteString("    fun toJson(): Map<String, Any?> {\n")
	sb.WriteString("        val map = mutableMapOf<String, Any?>()\n")
	for _, f := range fields {
		fmt.Fprintf(&sb, "        map[%s] = %s\n", strconv.Quote(f.wireName), r.toJsonExpr(f))
	}
	sb.WriteString("        return map\n")
	sb.WriteString("    }\n")

	sb.WriteString("\n    companion object {\n")
	fmt.Fprintf(&sb, "        fun fromJson(json: Map<String, Any?>): %s {\n", name)
	fmt.Fprintf(&sb, "            return %s(\n", name)
	for i, f := range fields {
		comma := ","
		if i == len(fields)-1 {
			comma = ""
		}
		fmt.Fprintf(&sb, "                %s = %s%s\n", f.propertyName, r.fromJsonExpr(f), comma)
	}
	sb.WriteString("            )\n")
	sb.WriteString("        }\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String(), extra, nil
}

// toJsonExpr renders the RHS of `map["<wire>"] = <expr>` for one field.
// Primitives serialize as themselves; a nested object serializes via its
// own toJson(); an enum serializes via its .value property (consulting
// r.refKind, which routes through r.nameFor before looking up r.kinds, to
// tell an object-shaped $ref from an enum-shaped one — see refKind's doc
// comment for why the raw ref name can't be used to key r.kinds
// directly); a List maps element-wise; every case has an optional variant
// using ?. instead of a direct call.
func (r *renderer) toJsonExpr(f resolvedField) string {
	access := f.propertyName
	if f.optional {
		access += "?"
	}
	switch f.node.Kind {
	case ir.KindRef:
		if r.refKind(f.node.RefName) == modelKindEnum {
			return access + ".value"
		}
		return access + ".toJson()"
	case ir.KindArray:
		// mapOp already supplies the optional "?." itself (the safe call
		// binds to the .map invocation, not to the property read), so this
		// uses f.propertyName directly rather than access — access already
		// carries its own "?" suffix, and combining the two would emit
		// "tags??.map { ... }", which is not valid Kotlin.
		return fmt.Sprintf("%s%s { %s }", f.propertyName, mapOp(f.optional), r.itemToJsonExpr(f.node.Array.Items))
	case ir.KindMap:
		// Same access-vs-f.propertyName reasoning as KindArray above.
		// "(_, it)" destructures the Map.Entry<String, V> and explicitly
		// names its value component "it", so itemToJsonExpr's generated
		// expressions (which reference "it") work unchanged for a map's
		// values, not just an array's elements.
		return fmt.Sprintf("%s%s { (_, it) -> %s }", f.propertyName, mapValuesOp(f.optional), r.itemToJsonExpr(f.node.Map.Values))
	default:
		return f.propertyName
	}
}

func mapOp(optional bool) string {
	if optional {
		return "?.map"
	}
	return ".map"
}

func mapValuesOp(optional bool) string {
	if optional {
		return "?.mapValues"
	}
	return ".mapValues"
}

// itemToJsonExpr renders the body of the `.map { ... }` lambda used to
// serialize one array element. item is already the fully-resolved
// (alias-unwrapped) node for this position — resolveType's ir.KindArray
// case built it by recursing into resolveType itself, which already
// routes any ir.KindRef through effectiveRefNode — so no further
// resolution is needed here, at any nesting depth. The ir.KindArray case
// below recurses for a nested array (List<List<T>>): "it" at that level
// is already the properly-typed List<T> value (not raw Any?, since
// toJson works outward from real typed values), so the inner .map{} call
// needs no cast, only the same per-element transform recursively.
func (r *renderer) itemToJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindRef:
		if r.refKind(item.RefName) == modelKindEnum {
			return "it.value"
		}
		return "it.toJson()"
	case ir.KindArray:
		return fmt.Sprintf("it.map { %s }", r.itemToJsonExpr(item.Array.Items))
	case ir.KindMap:
		return fmt.Sprintf("it.mapValues { (_, it) -> %s }", r.itemToJsonExpr(item.Map.Values))
	default:
		return "it"
	}
}

// fromJsonExpr renders the RHS of `<propertyName> = <expr>` inside
// fromJson's constructor call for one field. Casting a JSON number to
// the common Number supertype before converting (rather than casting
// straight to Int/Double) is deliberate: real-world JSON-to-Map parsers
// vary in whether a whole number comes through as Int, Long, or Double.
func (r *renderer) fromJsonExpr(f resolvedField) string {
	wire := strconv.Quote(f.wireName)
	switch f.node.Kind {
	case ir.KindPrimitive:
		return kotlinPrimitiveFromJsonExpr(f.node.Primitive, wire, f.optional)
	case ir.KindRef:
		target := r.nameFor(f.node.RefName)
		if r.refKind(f.node.RefName) == modelKindEnum {
			if f.optional {
				return fmt.Sprintf("(json[%s] as? String)?.let { %s.fromValue(it) }", wire, target)
			}
			return fmt.Sprintf("%s.fromValue(json[%s] as String)", target, wire)
		}
		if f.optional {
			return fmt.Sprintf("(json[%s] as? Map<String, Any?>)?.let { %s.fromJson(it) }", wire, target)
		}
		return fmt.Sprintf("%s.fromJson(json[%s] as Map<String, Any?>)", target, wire)
	case ir.KindArray:
		itemExpr := r.itemFromJsonExpr(f.node.Array.Items)
		if f.optional {
			return fmt.Sprintf("(json[%s] as? List<*>)?.map { %s }", wire, itemExpr)
		}
		return fmt.Sprintf("(json[%s] as List<*>).map { %s }", wire, itemExpr)
	case ir.KindMap:
		itemExpr := r.itemFromJsonExpr(f.node.Map.Values)
		if f.optional {
			return fmt.Sprintf("(json[%s] as? Map<String, Any?>)?.mapValues { (_, it) -> %s }", wire, itemExpr)
		}
		return fmt.Sprintf("(json[%s] as Map<String, Any?>).mapValues { (_, it) -> %s }", wire, itemExpr)
	default:
		return fmt.Sprintf("json[%s]", wire)
	}
}

func kotlinPrimitiveFromJsonExpr(p ir.Primitive, wire string, optional bool) string {
	switch p {
	case ir.PrimitiveBoolean:
		if optional {
			return fmt.Sprintf("json[%s] as? Boolean", wire)
		}
		return fmt.Sprintf("json[%s] as Boolean", wire)
	case ir.PrimitiveInteger:
		if optional {
			return fmt.Sprintf("(json[%s] as? Number)?.toInt()", wire)
		}
		return fmt.Sprintf("(json[%s] as Number).toInt()", wire)
	case ir.PrimitiveNumber:
		if optional {
			return fmt.Sprintf("(json[%s] as? Number)?.toDouble()", wire)
		}
		return fmt.Sprintf("(json[%s] as Number).toDouble()", wire)
	default: // ir.PrimitiveString and any unexpected value both read as a string
		if optional {
			return fmt.Sprintf("json[%s] as? String", wire)
		}
		return fmt.Sprintf("json[%s] as String", wire)
	}
}

// itemFromJsonExpr renders the body of the `.map { ... }` lambda used to
// deserialize one array element. item is already the fully-resolved
// (alias-unwrapped) node for this position, for the same reason
// itemToJsonExpr's doc comment explains. The ir.KindArray case recurses
// for a nested array (List<List<T>>): "it" at that level is raw Any?
// (from the outer `(json[...] as List<*>).map { ... }`), so it must be
// cast to List<*> before the inner .map{} can run over it — mirroring
// fromJsonExpr's own top-level ir.KindArray handling one level down.
func (r *renderer) itemFromJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindPrimitive:
		switch item.Primitive {
		case ir.PrimitiveBoolean:
			return "it as Boolean"
		case ir.PrimitiveInteger:
			return "(it as Number).toInt()"
		case ir.PrimitiveNumber:
			return "(it as Number).toDouble()"
		default:
			return "it as String"
		}
	case ir.KindRef:
		target := r.nameFor(item.RefName)
		if r.refKind(item.RefName) == modelKindEnum {
			return fmt.Sprintf("%s.fromValue(it as String)", target)
		}
		return fmt.Sprintf("%s.fromJson(it as Map<String, Any?>)", target)
	case ir.KindArray:
		return fmt.Sprintf("(it as List<*>).map { %s }", r.itemFromJsonExpr(item.Array.Items))
	case ir.KindMap:
		return fmt.Sprintf("(it as Map<String, Any?>).mapValues { (_, it) -> %s }", r.itemFromJsonExpr(item.Map.Values))
	default:
		return "it"
	}
}
