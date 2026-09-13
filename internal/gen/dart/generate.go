package dart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tarikomercehajic/openapi2code/internal/gen/mobile"
	"github.com/tarikomercehajic/openapi2code/internal/ir"
)

// ModelOutput is one generated top-level Dart declaration: a class, an
// enhanced enum, or a typedef. Dependencies lists the other declarations
// (by their Dart type name) this one's declaration text references —
// needed because, unlike Swift/Kotlin, Dart requires an explicit import
// between files even within the same package.
type ModelOutput struct {
	Name         string
	Declaration  string
	Dependencies []string
}

type extraDecl = mobile.PendingDecl

type modelKind int

const (
	modelKindObject modelKind = iota
	modelKindEnum
	modelKindOther
)

// Generate renders every model in doc as a Dart declaration, plus any
// synthesized for inline enums/objects — the same structure as
// internal/gen/swift and internal/gen/kotlin's Generate, independently
// implemented for Dart's syntax and serialization approach.
// Generate mints every synthesized declaration's name and classifies its
// modelKind at the exact moment resolveType creates it (see resolveType's
// doc comment) — not later, in this function's own extra-processing loop.
// Re-deriving either here instead would be a real bug: a declaration's own
// toJsonExpr/fromJsonExpr runs synchronously, before this loop ever reaches
// the matching extra entry, and already needs r.kinds populated for any
// inline enum/object that declaration just synthesized.
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
		decl, deps, extra, err := r.renderDeclaration(item.Name, item.Node)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", item.Name, err)
		}
		if decl == "" {
			continue
		}
		outputs = append(outputs, ModelOutput{Name: item.Name, Declaration: decl, Dependencies: dedupeSorted(removeName(deps, item.Name))})
		// Each e is already final and globally unique, and r.kinds[e.Name]
		// is already populated — resolveType did both at the moment it
		// synthesized this extraDecl. Do not re-derive either here.
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

// modelsByName looks up a document's top-level models by their IR name —
// so flattenFields can resolve an allOf-derived Extends chain into the
// actual base model's Fields.
type renderer struct {
	names        map[string]string
	kinds        map[string]modelKind
	modelsByName map[string]*ir.Model

	// used tracks every Dart type identifier minted so far: every
	// top-level model's assigned name (seeded above before any rendering
	// starts) plus every synthesized inline enum/object name. resolveType
	// consults and updates this directly, at the exact moment it mints a
	// new synthesized name, so that name — and its r.kinds entry — are
	// decided exactly once. See resolveType's doc comment.
	used map[string]bool
}

// flattenFields resolves o's Extends chain (from allOf) into a single
// field list, per the design's "allOf always flattens" rule — Dart's
// single-inheritance `extends` can't represent a multi-member allOf
// chain anyway, so a base's fields are copied in directly instead. This
// is required, not optional: internal/ir's buildAllOfNode records a
// $ref allOf member only in Extends, never in Fields — only inline
// object members land in Fields, so o.Fields alone is NOT already
// flattened. Extends targets are walked first, in order (for a
// readable field ordering), but o's OWN fields always win a name
// collision — matching internal/gen/ts's `extends` and internal/gen/zod's
// `.extend()`, both of which let a derived schema's own field shadow an
// inherited one of the same name on the identical spec. (An earlier
// version of this function gave the base precedence instead, which is
// wrong; ownNames below is what fixes it.)
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
	// o.Fields is appended unconditionally, not through addBase's dedup
	// — ownNames above already guarantees nothing already in fields
	// shares a name with it.
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

func (r *renderer) renderDeclaration(name string, node *ir.Node) (decl string, deps []string, extra []extraDecl, err error) {
	switch node.Kind {
	case ir.KindObject:
		d, deps, extra := r.renderClass(name, node.Object)
		return d, deps, extra, nil
	case ir.KindEnum:
		return r.renderEnum(name, node.Enum), nil, nil, nil
	case ir.KindUnion, ir.KindIntersection:
		return unsupportedShapeComment(name), nil, nil, nil
	case ir.KindPrimitive:
		if node.Primitive == ir.PrimitiveUnknown {
			return unsupportedShapeComment(name), nil, nil, nil
		}
		return fmt.Sprintf("typedef %s = %s;\n", name, renderPrimitive(node.Primitive)), nil, nil, nil
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
			return unsupportedShapeComment(name), nil, nil, nil
		}
		return fmt.Sprintf("typedef %s = %s;\n", name, expr), r.collectRefDeps(node), extra, nil
	case ir.KindMap:
		// Same name+"Item" seeding as KindArray above, for the same
		// self-reference reason (this model's own type IS the map).
		expr, _, skip, extra := r.resolveType(node, name+"Item")
		if skip {
			return unsupportedShapeComment(name), nil, nil, nil
		}
		return fmt.Sprintf("typedef %s = %s;\n", name, expr), r.collectRefDeps(node), extra, nil
	case ir.KindRef:
		if r.refIsUnsupported(node.RefName) {
			return unsupportedShapeComment(name), nil, nil, nil
		}
		target := r.nameFor(node.RefName)
		return fmt.Sprintf("typedef %s = %s;\n", name, target), []string{target}, nil, nil
	default:
		return "", nil, nil, nil
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
// call from every field/typedef that $refs the same target without
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
// place of name's real declaration when its shape has no clean Dart
// representation (oneOf/anyOf, an allOf mixing object and non-object
// members, or an unrecognized primitive type) — per the design spec's
// "skipped ... with a comment explaining why" rule. Mirrors
// internal/gen/swift's identically-named helper.
func unsupportedShapeComment(name string) string {
	return fmt.Sprintf("// %s was not generated for the Dart target: unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type).\n", name)
}

// collectRefDeps walks node for every KindRef it contains (directly or
// through arrays), for the rare top-level array-of-ref typedef case.
// Every RefName is routed through r.nameFor before being returned: a raw
// IR schema name only agrees with the resolved Dart identifier
// pkg/engine's fileNameByModel is keyed by when the schema name needs no
// sanitization and never collided with anything — otherwise the raw name
// silently fails to match any file and produces a broken `import ”;`
// (this is the same raw-name-vs-resolved-name class of bug refKind's own
// doc comment already documents for r.kinds; here it manifests as a
// missing/blank import instead of a mis-dispatch).
func (r *renderer) collectRefDeps(node *ir.Node) []string {
	switch node.Kind {
	case ir.KindRef:
		return []string{r.nameFor(node.RefName)}
	case ir.KindArray:
		return r.collectRefDeps(node.Array.Items)
	case ir.KindMap:
		return r.collectRefDeps(node.Map.Values)
	default:
		return nil
	}
}

func renderPrimitive(p ir.Primitive) string {
	switch p {
	case ir.PrimitiveString:
		return "String"
	case ir.PrimitiveNumber:
		return "double"
	case ir.PrimitiveInteger:
		return "int"
	case ir.PrimitiveBoolean:
		return "bool"
	default:
		return "String"
	}
}

// renderEnum renders e as an enhanced enum carrying its wire string in
// `value`. Value identifiers are uniquified within this one enum (used is
// local to this call): two DIFFERENT wire values can converge on the same
// identifier under ToCamelCase (e.g. "photo_urls" and "photoUrls" both
// becoming "photoUrls"), which would otherwise emit two identically-named
// enum values — a compile error. The constructor argument is always the
// value's OWN original wire string, so the disambiguating suffix never
// changes what fromValue()/`.value` see.
func (r *renderer) renderEnum(name string, e *ir.EnumNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "enum %s {\n", name)
	used := make(map[string]bool, len(e.Values))
	for i, v := range e.Values {
		valueName := mobile.Uniquify(SanitizeEnumValueIdentifier(v), used)
		used[valueName] = true
		comma := ","
		if i == len(e.Values)-1 {
			comma = ";"
		}
		fmt.Fprintf(&sb, "    %s(%s)%s\n", valueName, strconv.Quote(v), comma)
	}
	sb.WriteString("\n    final String value;\n")
	fmt.Fprintf(&sb, "    const %s(this.value);\n\n", name)
	fmt.Fprintf(&sb, "    static %s fromValue(String value) => %s.values.firstWhere((e) => e.value == value);\n", name, name)
	sb.WriteString("}\n")
	return sb.String()
}

type resolvedField struct {
	propertyName string
	wireName     string
	typeExpr     string
	node         *ir.Node
	optional     bool
}

// resolveFields resolves every field of an object into resolvedFields,
// synthesizing a top-level name for any inline enum/object it encounters.
// skipped carries the wire (original JSON) name of every field omitted
// because resolveType found no representable Dart type for it — per the
// design's unsupported-shape rule, the caller surfaces these as comments
// rather than dropping them without a trace (mirrors internal/gen/swift's
// resolveFields).
//
// deps is collected from BOTH the field's original (pre-alias-resolution)
// type node, via typeRefNames, AND the alias-resolved effective node, via
// refNamesIn — not effective alone. A field's *declared* Dart type always
// comes from expr/f.Type (the alias name itself, e.g. `PetId`, unresolved),
// while effective is only what toJson()/fromJson() dispatch on internally
// (e.g. the underlying String for a PetId alias, which contributes no dep
// at all). Without typeRefNames, a field typed as a scalar alias would
// need no import for that alias's own declaration file, and a field typed
// as a named array alias (`Categories = List<Category>`) would only pick
// up a dependency on the element type (Category, via effective) and never
// on Categories itself — both leave the modular output missing an import
// its declared field type actually needs. For an inline synthesized
// enum/object, f.Type.Kind is KindEnum/KindObject (not KindRef), so
// typeRefNames contributes nothing there and refNamesIn(effective) alone
// already correctly captures the synthesized name via the ref resolveType
// built for it — so combining both is safe and never double-counts a
// dependency that matters (any duplicate is deduplicated by Generate's
// dedupeSorted).
//
// Property identifiers are uniquified within this one class (used is
// local to this call, so nothing leaks across classes): two DIFFERENT
// wire names can converge on the same identifier under ToCamelCase (e.g.
// "photo_urls" and "photoUrls" both becoming "photoUrls"), which would
// otherwise emit two identically-named fields — a compile error. This
// runs on the ALREADY-FLATTENED field list, so it also covers an
// inherited-via-allOf field colliding with an own field this way.
// flattenFields handles the different concern of two fields sharing an
// IDENTICAL wire name. wireName always stays the field's own original
// name, so toJson()/fromJson() still read and write the right JSON key.
func (r *renderer) resolveFields(containingTypeName string, fields []*ir.Field) (resolved []resolvedField, deps []string, allExtra []extraDecl, skipped []string) {
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
		deps = append(deps, r.typeRefNames(f.Type)...)
		deps = append(deps, r.refNamesIn(effective)...)
		allExtra = append(allExtra, extra...)
	}
	return resolved, deps, allExtra, skipped
}

// typeRefNames walks node — a field's ORIGINAL, pre-alias-resolution type
// — for every $ref it contains (directly or through arrays), returning
// each as its resolved Dart identifier (routed through r.nameFor, same
// reasoning as collectRefDeps/refNamesIn). See resolveFields' doc comment
// for why this must be collected in addition to refNamesIn(effective),
// not instead of it.
func (r *renderer) typeRefNames(node *ir.Node) []string {
	switch node.Kind {
	case ir.KindRef:
		return []string{r.nameFor(node.RefName)}
	case ir.KindArray:
		return r.typeRefNames(node.Array.Items)
	case ir.KindMap:
		return r.typeRefNames(node.Map.Values)
	default:
		return nil
	}
}

// refNamesIn walks node — a field's alias-resolved effective dispatch
// node (see resolveType/effectiveRefNode) — for every $ref it contains
// (directly or through arrays). Every RefName is routed through
// r.nameFor before being returned; see collectRefDeps' doc comment for
// why a raw IR schema name can't be used directly here.
func (r *renderer) refNamesIn(node *ir.Node) []string {
	switch node.Kind {
	case ir.KindRef:
		return []string{r.nameFor(node.RefName)}
	case ir.KindArray:
		return r.refNamesIn(node.Array.Items)
	case ir.KindMap:
		return r.refNamesIn(node.Map.Values)
	default:
		return nil
	}
}

// resolveType resolves node to a non-optional Dart type expression.
// suggestedName is the name to synthesize if node turns out to be an
// inline (non-$ref) enum or object. skip is true when node has no
// representable Dart type (a union, an allOf-with-non-object-member
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

// refKind reports the modelKind of the top-level model named refName — the
// raw IR schema name, exactly as stored on an ir.Node's RefName field.
// r.kinds itself is keyed by the resolved Dart identifier (see Generate and
// resolveType's ir.KindEnum/ir.KindObject case), so a raw name must always
// be routed through r.nameFor before it can be used to key into r.kinds.
// Looking r.kinds up directly by a raw schema name only agrees with this
// when that name happens to need no sanitization and never collided with
// anything — a schema literally named "Pet-Status", one needing a
// reserved-word suffix, or one that lost a name collision to a same-named
// sibling (picking up a "_2" suffix) would all silently mismatch and fall
// through to the wrong (object) dispatch branch. (The identical fix
// applies in internal/gen/kotlin, for the same reason.)
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
// against a malformed/cyclic alias chain; ok is false when refName isn't a
// known model, has no recorded type, or the chain is cyclic.
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
// (`typedef Tags = List<Tag>;`), a named primitive alias (`typedef PetId
// = String;`), or a further $ref (alias-to-alias). Dart's typedef is
// structurally transparent — it IS the underlying type at compile time,
// not a distinct wrapper type — so a field of that type must be
// (de)serialized exactly as if it held the underlying concrete shape
// directly, not as an object with its own toJson()/fromJson(). This walks
// the alias chain via resolveAliasTarget to find that shape:
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
//     default, generating `ItemId.fromJson(...)`/`e.toJson()` for what is
//     actually a String. Recursing through this same function fixes
//     that. It does NOT fix the sibling case where the array alias's
//     items are an INLINE (non-$ref) object or enum — e.g. `Things:
//     {type: array, items: {type: object, ...}}` — because that item type
//     was synthesized under a fresh name (e.g. "Things_2") back when
//     Things was originally rendered as its own top-level declaration (in
//     a separate, earlier call to resolveType), and that name isn't
//     recoverable here: re-deriving it independently would risk the two
//     derivations disagreeing, exactly the bug class this generator's
//     $ref-dispatch fixes were about. A $ref to such an array alias
//     therefore still declares the correct field type (the alias name
//     itself) but its toJson()/fromJson() will incorrectly treat the
//     inline item type as a plain value rather than calling its
//     (correctly-generated, just unreferenced from here) toJson()/
//     fromJson() — a known, disclosed gap, not silently wrong in a way
//     that also affects the item type's own declaration;
//   - a concrete object or enum reached through one or more aliases needs
//     its OWN name substituted in (not the alias's): fromJsonExpr and
//     itemFromJsonExpr call target.fromJson(...)/target.fromValue(...), and
//     only the real object/enum type has that factory/static method — a
//     typedef has none of its own to call those on;
//   - a chain that bottoms out unresolvable (an unknown or cyclic
//     reference) or at a union/intersection — itself already an
//     unsupported shape rendered as a comment-only declaration with no
//     toJson/fromJson to call — falls back to node unchanged: the prior
//     (object-shaped) dispatch this fix does not attempt to make correct
//     for a case with no clean Dart representation either way.
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

// renderClass renders o as a class. o.Fields alone is not yet flattened
// for an allOf whose members include a $ref (see flattenFields), so this
// resolves the full field list through flattenFields rather than reading
// o.Fields directly.
// When every field was skipped (or there were none to begin with), this
// does NOT emit `Name({\n});`: Dart's grammar requires a named-parameter
// list inside `{}` to have at least one parameter — an empty `{}` list is
// a syntax error, not merely a constructor with no arguments — so a
// zero-field model falls back to a plain parameterless constructor
// instead, which needs no parameter list at all (mirrors
// internal/gen/kotlin's identical guard in renderDataClass).
func (r *renderer) renderClass(name string, o *ir.ObjectNode) (string, []string, []extraDecl) {
	fields, deps, extra, skipped := r.resolveFields(name, r.flattenFields(o))

	var sb strings.Builder
	fmt.Fprintf(&sb, "class %s {\n", name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "    final %s %s;\n", f.typeExpr, f.propertyName)
	}
	for _, wireName := range skipped {
		fmt.Fprintf(&sb, "    // %s: skipped — unsupported schema shape (oneOf/anyOf, an allOf mixing object and non-object members, or an unrecognized primitive type)\n", wireName)
	}

	if len(fields) == 0 {
		fmt.Fprintf(&sb, "\n    %s();\n", name)

		fmt.Fprintf(&sb, "\n    factory %s.fromJson(Map<String, dynamic> json) {\n", name)
		fmt.Fprintf(&sb, "        return %s();\n", name)
		sb.WriteString("    }\n")

		sb.WriteString("\n    Map<String, dynamic> toJson() {\n")
		sb.WriteString("        return {};\n")
		sb.WriteString("    }\n")
		sb.WriteString("}\n")
		return sb.String(), deps, extra
	}

	sb.WriteString("\n    ")
	fmt.Fprintf(&sb, "%s({\n", name)
	for _, f := range fields {
		required := "required "
		if f.optional {
			required = ""
		}
		fmt.Fprintf(&sb, "        %sthis.%s,\n", required, f.propertyName)
	}
	sb.WriteString("    });\n")

	sb.WriteString("\n    ")
	fmt.Fprintf(&sb, "factory %s.fromJson(Map<String, dynamic> json) {\n", name)
	fmt.Fprintf(&sb, "        return %s(\n", name)
	for _, f := range fields {
		fmt.Fprintf(&sb, "            %s: %s,\n", f.propertyName, r.fromJsonExpr(f))
	}
	sb.WriteString("        );\n")
	sb.WriteString("    }\n")

	sb.WriteString("\n    Map<String, dynamic> toJson() {\n")
	sb.WriteString("        return {\n")
	for _, f := range fields {
		fmt.Fprintf(&sb, "            %s: %s,\n", strconv.Quote(f.wireName), r.toJsonExpr(f))
	}
	sb.WriteString("        };\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return sb.String(), deps, extra
}

// toJsonExpr renders the RHS of `'<wire>': <expr>` inside toJson()'s
// returned map literal for one field. Dart's Map<String, dynamic>
// accepts any value type directly, so a primitive field needs no cast at
// all here — only a nested object/enum/array needs an explicit
// expression. Consults r.refKind, which routes through r.nameFor before
// looking up r.kinds, to tell an object-shaped $ref from an enum-shaped
// one — see refKind's doc comment for why the raw ref name can't be used
// to key r.kinds directly.
func (r *renderer) toJsonExpr(f resolvedField) string {
	switch f.node.Kind {
	case ir.KindRef:
		access := f.propertyName
		if f.optional {
			access += "?"
		}
		if r.refKind(f.node.RefName) == modelKindEnum {
			return access + ".value"
		}
		return access + ".toJson()"
	case ir.KindArray:
		access := f.propertyName
		if f.optional {
			access += "?"
		}
		return fmt.Sprintf("%s.map((e) => %s).toList()", access, r.itemToJsonExpr(f.node.Array.Items))
	case ir.KindMap:
		// Dart's Map.map() must return a MapEntry, unlike Kotlin's
		// mapValues (which keeps the key implicitly) — the key parameter
		// is just threaded through unchanged, and the value parameter is
		// named `e` so itemToJsonExpr's generated expressions (which
		// reference `e`) work unchanged for a map's values, not just an
		// array's elements.
		access := f.propertyName
		if f.optional {
			access += "?"
		}
		return fmt.Sprintf("%s.map((key, e) => MapEntry(key, %s))", access, r.itemToJsonExpr(f.node.Map.Values))
	default:
		return f.propertyName
	}
}

// itemToJsonExpr renders the body of the `.map((e) => ...)` closure used
// to serialize one array element. item is already the fully-resolved
// (alias-unwrapped) node for this position — resolveType's ir.KindArray
// case built it by recursing into resolveType itself, which already
// routes any ir.KindRef through effectiveRefNode — so no further
// resolution is needed here, at any nesting depth. The ir.KindArray case
// below recurses for a nested array (List<List<T>>): reusing the
// parameter name `e` at each nesting level shadows the outer one, which
// is legal Dart and never a problem here since the inner closure never
// needs to reference an outer level's element.
func (r *renderer) itemToJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindRef:
		if r.refKind(item.RefName) == modelKindEnum {
			return "e.value"
		}
		return "e.toJson()"
	case ir.KindArray:
		return fmt.Sprintf("e.map((e) => %s).toList()", r.itemToJsonExpr(item.Array.Items))
	case ir.KindMap:
		return fmt.Sprintf("e.map((key, e) => MapEntry(key, %s))", r.itemToJsonExpr(item.Map.Values))
	default:
		return "e"
	}
}

// fromJsonExpr renders the RHS of `<propertyName>: <expr>` inside
// fromJson's constructor call for one field.
func (r *renderer) fromJsonExpr(f resolvedField) string {
	wire := fmt.Sprintf("json[%s]", strconv.Quote(f.wireName))
	switch f.node.Kind {
	case ir.KindPrimitive:
		return dartPrimitiveFromJsonExpr(f.node.Primitive, wire, f.optional)
	case ir.KindRef:
		target := r.nameFor(f.node.RefName)
		if r.refKind(f.node.RefName) == modelKindEnum {
			if f.optional {
				return fmt.Sprintf("%s != null ? %s.fromValue(%s as String) : null", wire, target, wire)
			}
			return fmt.Sprintf("%s.fromValue(%s as String)", target, wire)
		}
		if f.optional {
			return fmt.Sprintf("%s != null ? %s.fromJson(%s as Map<String, dynamic>) : null", wire, target, wire)
		}
		return fmt.Sprintf("%s.fromJson(%s as Map<String, dynamic>)", target, wire)
	case ir.KindArray:
		itemExpr := r.itemFromJsonExpr(f.node.Array.Items)
		if f.optional {
			return fmt.Sprintf("(%s as List<dynamic>?)?.map((e) => %s).toList()", wire, itemExpr)
		}
		return fmt.Sprintf("(%s as List<dynamic>).map((e) => %s).toList()", wire, itemExpr)
	case ir.KindMap:
		itemExpr := r.itemFromJsonExpr(f.node.Map.Values)
		if f.optional {
			return fmt.Sprintf("(%s as Map<String, dynamic>?)?.map((key, e) => MapEntry(key, %s))", wire, itemExpr)
		}
		return fmt.Sprintf("(%s as Map<String, dynamic>).map((key, e) => MapEntry(key, %s))", wire, itemExpr)
	default:
		return wire
	}
}

// dartPrimitiveFromJsonExpr casts a raw JSON value to its Dart primitive
// type. A number goes through Dart's num supertype before converting
// explicitly: a JSON number with no decimal point decodes as a Dart int,
// not double, so a direct `as double` cast fails on whole-number values.
func dartPrimitiveFromJsonExpr(p ir.Primitive, wire string, optional bool) string {
	switch p {
	case ir.PrimitiveBoolean:
		if optional {
			return wire + " as bool?"
		}
		return wire + " as bool"
	case ir.PrimitiveInteger:
		if optional {
			return wire + " as int?"
		}
		return wire + " as int"
	case ir.PrimitiveNumber:
		if optional {
			return fmt.Sprintf("(%s as num?)?.toDouble()", wire)
		}
		return fmt.Sprintf("(%s as num).toDouble()", wire)
	default: // ir.PrimitiveString and any unexpected value both read as a string
		if optional {
			return wire + " as String?"
		}
		return wire + " as String"
	}
}

// itemFromJsonExpr renders the body of the `.map((e) => ...)` closure
// used to deserialize one array element. item is already the
// fully-resolved (alias-unwrapped) node for this position, for the same
// reason itemToJsonExpr's doc comment explains. The ir.KindArray case
// recurses for a nested array (List<List<T>>): `e` at that level is raw
// dynamic (from the outer `(json[...] as List<dynamic>).map(...)`), so it
// must be cast to List<dynamic> before the inner .map(...) can run over
// it — mirroring fromJsonExpr's own top-level ir.KindArray handling one
// level down. Reusing the parameter name `e` at each nesting level is
// legal Dart (shadowing), same reasoning as itemToJsonExpr.
func (r *renderer) itemFromJsonExpr(item *ir.Node) string {
	switch item.Kind {
	case ir.KindPrimitive:
		switch item.Primitive {
		case ir.PrimitiveBoolean:
			return "e as bool"
		case ir.PrimitiveInteger:
			return "e as int"
		case ir.PrimitiveNumber:
			return "(e as num).toDouble()"
		default:
			return "e as String"
		}
	case ir.KindRef:
		target := r.nameFor(item.RefName)
		if r.refKind(item.RefName) == modelKindEnum {
			return fmt.Sprintf("%s.fromValue(e as String)", target)
		}
		return fmt.Sprintf("%s.fromJson(e as Map<String, dynamic>)", target)
	case ir.KindArray:
		return fmt.Sprintf("(e as List<dynamic>).map((e) => %s).toList()", r.itemFromJsonExpr(item.Array.Items))
	case ir.KindMap:
		return fmt.Sprintf("(e as Map<String, dynamic>).map((key, e) => MapEntry(key, %s))", r.itemFromJsonExpr(item.Map.Values))
	default:
		return "e"
	}
}
