package ir

import (
	"fmt"
	"sort"

	"github.com/tarikomercehajic/openapi2code/internal/spec"
)

// maxNestingDepth caps how deeply buildNode will recurse into a schema's
// own structure (array items, object fields, allOf members, union
// variants) before giving up with a clean error. It exists to protect
// the WASM build, where a JS engine's call stack is far smaller than a
// native Go binary's: a spec nested ~3000 levels deep overflows the
// WASM call stack as an uncaught JS RangeError, while the same spec
// builds fine natively (Go's stacks grow). 500 is comfortably past any
// real-world spec (every fixture and benchmark spec in this repo is
// under 50 levels) and comfortably short of that browser failure mode.
const maxNestingDepth = 500

// Build walks a validated RawDocument and produces the IR Document for it.
func Build(raw *spec.RawDocument) (*Document, error) {
	names := make([]string, 0, len(raw.Schemas))
	for name := range raw.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	models := make([]*Model, 0, len(names))
	for _, name := range names {
		node, err := buildNode(raw, raw.Schemas[name], 0)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		models = append(models, &Model{Name: name, Type: node})
	}

	doc := &Document{Models: models}
	markCycles(doc)
	return doc, nil
}

func buildNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	if s == nil {
		return nil, fmt.Errorf("schema is null")
	}
	if depth > maxNestingDepth {
		return nil, fmt.Errorf("schema nesting exceeds maximum depth of %d", maxNestingDepth)
	}
	if s.Ref != "" {
		name, err := raw.ResolveRefName(s.Ref)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KindRef, RefName: name}, nil
	}
	if len(s.AllOf) > 0 {
		return buildAllOfNode(raw, s, depth)
	}
	if len(s.OneOf) > 0 {
		return buildUnionNode(raw, s.OneOf, depth)
	}
	if len(s.AnyOf) > 0 {
		return buildUnionNode(raw, s.AnyOf, depth)
	}
	if len(s.Enum) > 0 {
		return buildEnumNode(s), nil
	}
	if s.TypeUnrepresentable {
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}, nil
	}
	switch s.Type {
	case "array":
		return buildArrayNode(raw, s, depth)
	case "string":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveString}, nil
	case "number":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveNumber}, nil
	case "integer":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveInteger}, nil
	case "boolean":
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveBoolean}, nil
	case "object", "":
		if isMapShaped(s) {
			return buildMapNode(raw, s, depth)
		}
		return buildObjectNode(raw, s, depth)
	default:
		return &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}, nil
	}
}

// isMapShaped reports whether s should build as a KindMap (a dictionary
// type) rather than an ordinary KindObject. Scoped deliberately narrow:
//
//   - additionalProperties must be the schema-object form (a bare `true`
//     has no well-defined value type to generate consistently across all
//     five targets — Swift in particular has no natural "any JSON value"
//     Codable type without a hand-rolled wrapper this project doesn't
//     have — so it's left as an ordinary, zero-field object, same as
//     `additionalProperties: false` or an absent additionalProperties);
//   - s must declare no properties of its own. A schema with BOTH fixed
//     properties AND additionalProperties (a struct with a catch-all
//     bucket) is a real, rarer pattern this generator doesn't attempt:
//     it renders as an ordinary object using only the fixed properties,
//     same as if additionalProperties were absent — see
//     docs/superpowers/specs/2026-09-10-future-considerations.md.
func isMapShaped(s *spec.RawSchema) bool {
	return len(s.Properties) == 0 && s.AdditionalProperties != nil && s.AdditionalProperties.Schema != nil
}

func buildMapNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	values, err := buildNode(raw, s.AdditionalProperties.Schema, depth+1)
	if err != nil {
		return nil, fmt.Errorf("additionalProperties: %w", err)
	}
	return &Node{Kind: KindMap, Map: &MapNode{Values: values}}, nil
}

// buildEnumNode renders every enum value to its string representation
// (Values) and records the schema's declared type on EnumNode.Primitive
// so generators know whether that string is a quoted string literal or
// the text of a numeric/boolean literal — s.Type is trusted directly,
// the same source buildNode's own primitive switch uses, and an enum
// with no declared "type" (legal in JSON Schema) defaults to
// PrimitiveString, matching every value's rendering before this
// Primitive field existed.
//
// A literal null entry (the standard OpenAPI 3.0 workaround for a
// nullable enum, paired with nullable: true, since 3.0 has no
// `type: [T, null]`) is dropped rather than stringified: fmt.Sprint on a
// nil interface{} always yields the literal text "<nil>", which would
// otherwise leak into every generator's output as a bogus enum member.
// The field/schema already conveys nullability separately (see
// RawSchema.IsNullable / Field.Nullable), so no information is lost.
func buildEnumNode(s *spec.RawSchema) *Node {
	primitive := PrimitiveString
	switch s.Type {
	case "integer":
		primitive = PrimitiveInteger
	case "number":
		primitive = PrimitiveNumber
	case "boolean":
		primitive = PrimitiveBoolean
	}
	values := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		if v == nil {
			continue
		}
		values = append(values, fmt.Sprint(v))
	}
	return &Node{Kind: KindEnum, Enum: &EnumNode{Values: values, Primitive: primitive}}
}

func buildArrayNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	if s.Items == nil {
		return &Node{Kind: KindArray, Array: &ArrayNode{Items: &Node{Kind: KindPrimitive, Primitive: PrimitiveUnknown}}}, nil
	}
	items, err := buildNode(raw, s.Items, depth+1)
	if err != nil {
		return nil, fmt.Errorf("items: %w", err)
	}
	return &Node{Kind: KindArray, Array: &ArrayNode{Items: items}}, nil
}

func buildObjectNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]*Field, 0, len(names))
	for _, name := range names {
		propSchema := s.Properties[name]
		fieldType, err := buildNode(raw, propSchema, depth+1)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, &Field{
			Name:     name,
			Type:     fieldType,
			Optional: !requiredSet[name],
			Nullable: propSchema.IsNullable(),
		})
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Fields: fields}}, nil
}

// schemaIsObjectShaped reports whether s ultimately resolves to an
// object-shaped node (what buildNode would build as KindObject), by the
// same precedence buildNode uses, without fully building it — it's used
// to decide whether a $ref allOf member is safe to add to Extends or
// must force the whole allOf to fall back to KindIntersection.
//
// visited guards against a cyclic ref spine (e.g. two models that
// mutually allOf-extend each other, an existing supported pattern — see
// markCycles/Model.Cyclic). Revisiting a ref already on the current
// walk means no non-object shape has been found along that path, so it
// resolves optimistically to true rather than forcing a fallback merely
// because the walk looped — matching cycleWalk's onStack treatment in
// markCycles below.
func schemaIsObjectShaped(raw *spec.RawDocument, s *spec.RawSchema, visited map[string]bool) bool {
	if s == nil {
		return false
	}
	if s.Ref != "" {
		name, err := raw.ResolveRefName(s.Ref)
		if err != nil {
			return false
		}
		if visited[name] {
			return true
		}
		visited[name] = true
		return schemaIsObjectShaped(raw, raw.Schemas[name], visited)
	}
	if len(s.AllOf) > 0 {
		for _, member := range s.AllOf {
			if member == nil || !schemaIsObjectShaped(raw, member, visited) {
				return false
			}
		}
		return true
	}
	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 || len(s.Enum) > 0 {
		return false
	}
	switch s.Type {
	case "array", "string", "number", "integer", "boolean":
		return false
	case "object", "":
		return !isMapShaped(s)
	default:
		return false
	}
}

func buildAllOfNode(raw *spec.RawDocument, s *spec.RawSchema, depth int) (*Node, error) {
	// Collapse single-ref allOf with no own properties to a plain ref.
	// This handles the common pattern of allOf used to attach a description to a $ref.
	if len(s.AllOf) == 1 && s.AllOf[0] != nil && s.AllOf[0].Ref != "" {
		own, err := buildObjectNode(raw, s, depth)
		if err != nil {
			return nil, fmt.Errorf("allOf own properties: %w", err)
		}
		if len(own.Object.Fields) == 0 {
			name, err := raw.ResolveRefName(s.AllOf[0].Ref)
			if err != nil {
				return nil, fmt.Errorf("allOf[0]: %w", err)
			}
			return &Node{Kind: KindRef, RefName: name}, nil
		}
	}

	hasNonObject := false
	memberNodes := make([]*Node, 0, len(s.AllOf))
	var extends []string
	var fields []*Field
	seen := make(map[string]bool)

	addFields := func(fs []*Field) {
		for _, f := range fs {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
	}

	for i, member := range s.AllOf {
		var node *Node
		if member != nil && member.Ref != "" {
			name, err := raw.ResolveRefName(member.Ref)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = &Node{Kind: KindRef, RefName: name}
			if schemaIsObjectShaped(raw, member, map[string]bool{}) {
				extends = append(extends, name)
			} else {
				hasNonObject = true
			}
		} else {
			built, err := buildNode(raw, member, depth+1)
			if err != nil {
				return nil, fmt.Errorf("allOf[%d]: %w", i, err)
			}
			node = built
			if node.Kind == KindObject {
				addFields(node.Object.Fields)
				extends = append(extends, node.Object.Extends...)
			} else {
				hasNonObject = true
			}
		}
		memberNodes = append(memberNodes, node)
	}

	own, err := buildObjectNode(raw, s, depth)
	if err != nil {
		return nil, fmt.Errorf("allOf own properties: %w", err)
	}
	addFields(own.Object.Fields)

	if hasNonObject {
		if len(own.Object.Fields) > 0 {
			memberNodes = append(memberNodes, own)
		}
		return &Node{Kind: KindIntersection, Intersection: &IntersectionNode{Members: memberNodes}}, nil
	}
	return &Node{Kind: KindObject, Object: &ObjectNode{Extends: extends, Fields: fields}}, nil
}

func buildUnionNode(raw *spec.RawDocument, variants []*spec.RawSchema, depth int) (*Node, error) {
	nodes := make([]*Node, 0, len(variants))
	for i, v := range variants {
		n, err := buildNode(raw, v, depth+1)
		if err != nil {
			return nil, fmt.Errorf("variant[%d]: %w", i, err)
		}
		nodes = append(nodes, n)
	}
	return &Node{Kind: KindUnion, Union: &UnionNode{Variants: nodes}}, nil
}

// markCycles sets Cyclic on every model that is reachable from itself by
// following Ref edges through other models' type trees.
// markCycles flags every model that participates in a $ref cycle,
// including a cycle reached only through Object.Extends, an array's
// Items, a map's Values, or a union/intersection member.
//
// This used to run a separate reachability search per model ("does this
// model's own type reach a $ref back to itself"), memoizing per-search
// results that weren't path-dependent. That approach is exponential on a
// genuinely, densely cyclic graph (as opposed to a merely diamond-shaped
// acyclic one): any node whose exploration crosses a real cycle
// elsewhere in the graph can never be cached (its result depends on
// which path reached it), so a node reachable via many diamond paths
// gets fully re-explored, subtree and all, on every single path — this
// was measured taking 400+ seconds on a real, densely cross-referenced
// 1454-schema spec (Stripe's), against well under a second for a
// same-order-of-magnitude but far less interconnected spec.
//
// The fix reframes the problem as what it actually is: "which models lie
// on a cycle in the model-to-model $ref graph" is exactly what strongly
// connected components answer, in one O(V+E) pass over the whole
// document — no per-model restart, and so no repeated re-exploration of
// shared subtrees at all. A model is cyclic iff its SCC has more than
// one member, or is a single model with a direct self-edge.
func markCycles(doc *Document) {
	graph := make(map[string][]string, len(doc.Models))
	for _, m := range doc.Models {
		edgeSet := map[string]bool{}
		collectDirectRefs(m.Type, edgeSet)
		edges := make([]string, 0, len(edgeSet))
		for name := range edgeSet {
			edges = append(edges, name)
		}
		graph[m.Name] = edges
	}

	cyclic := make(map[string]bool, len(doc.Models))
	for _, scc := range stronglyConnectedComponents(graph) {
		if len(scc) > 1 {
			for _, name := range scc {
				cyclic[name] = true
			}
			continue
		}
		name := scc[0]
		for _, e := range graph[name] {
			if e == name {
				cyclic[name] = true
				break
			}
		}
	}

	for _, m := range doc.Models {
		m.Cyclic = cyclic[m.Name]
	}
}

// collectDirectRefs walks node for every $ref name it reaches without
// crossing into another model's own definition — Object.Extends, a
// field's type, an array's Items, a map's Values, and every union/
// intersection member are all followed; a KindRef itself is recorded and
// NOT expanded further, since that expansion is the referenced model's
// own edge set, computed separately when markCycles processes that
// model in turn. This is the one-hop edge relation strongly connected
// components needs; the transitive cycle detection is entirely the
// graph algorithm's job, not this function's.
func collectDirectRefs(node *Node, out map[string]bool) {
	if node == nil {
		return
	}
	switch node.Kind {
	case KindRef:
		out[node.RefName] = true
	case KindObject:
		for _, extend := range node.Object.Extends {
			out[extend] = true
		}
		for _, f := range node.Object.Fields {
			collectDirectRefs(f.Type, out)
		}
	case KindArray:
		collectDirectRefs(node.Array.Items, out)
	case KindMap:
		collectDirectRefs(node.Map.Values, out)
	case KindUnion:
		for _, v := range node.Union.Variants {
			collectDirectRefs(v, out)
		}
	case KindIntersection:
		for _, member := range node.Intersection.Members {
			collectDirectRefs(member, out)
		}
	}
}

// stronglyConnectedComponents computes the strongly connected components
// of graph (a node name -> its direct edge targets) via Tarjan's
// algorithm, in O(V+E) time regardless of how the graph is shaped —
// including a densely cyclic one, unlike the per-node reachability
// search this replaced. An edge to a name absent from graph's keys (a
// dangling reference) is harmless: it gets visited as a childless node
// and forms its own trivial, non-cyclic component.
//
// Implemented iteratively with an explicit frame stack rather than
// recursively: a long chain of $ref-linked models (thousands of nodes)
// would otherwise recurse as deeply as the chain is long, which is
// exactly the shape that has overflowed the WASM build's much smaller
// call stack before (see docs/superpowers/specs/2026-09-10-future-considerations.md).
// Each frame tracks its node and how far through that node's edge list
// it has gotten, standing in for the local variables an equivalent
// recursive call would keep on the native call stack.
func stronglyConnectedComponents(graph map[string][]string) [][]string {
	type frame struct {
		node     string
		children []string
		childIdx int
	}

	index := 0
	indices := make(map[string]int, len(graph))
	lowlink := make(map[string]int, len(graph))
	onStack := make(map[string]bool, len(graph))
	var stack []string
	var sccs [][]string

	roots := make([]string, 0, len(graph))
	for name := range graph {
		roots = append(roots, name)
	}
	sort.Strings(roots) // deterministic component discovery order

	for _, root := range roots {
		if _, seen := indices[root]; seen {
			continue
		}

		indices[root] = index
		lowlink[root] = index
		index++
		stack = append(stack, root)
		onStack[root] = true
		call := []*frame{{node: root, children: graph[root]}}

		for len(call) > 0 {
			top := call[len(call)-1]
			if top.childIdx < len(top.children) {
				child := top.children[top.childIdx]
				top.childIdx++
				if _, seen := indices[child]; !seen {
					indices[child] = index
					lowlink[child] = index
					index++
					stack = append(stack, child)
					onStack[child] = true
					call = append(call, &frame{node: child, children: graph[child]})
				} else if onStack[child] && indices[child] < lowlink[top.node] {
					lowlink[top.node] = indices[child]
				}
				continue
			}

			call = call[:len(call)-1]
			if len(call) > 0 {
				parent := call[len(call)-1]
				if lowlink[top.node] < lowlink[parent.node] {
					lowlink[parent.node] = lowlink[top.node]
				}
			}
			if lowlink[top.node] == indices[top.node] {
				var scc []string
				for {
					n := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[n] = false
					scc = append(scc, n)
					if n == top.node {
						break
					}
				}
				sccs = append(sccs, scc)
			}
		}
	}
	return sccs
}
