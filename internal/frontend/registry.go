package frontend

// Registry is the resource + reference graph produced by resolution (design §10, §19):
// resolved $ref/$anchor/$id targets, and strongly-connected-component analysis
// classifying guarded vs unguarded recursion. Populated by [Load] / [FromLibOpenAPI].
type Registry struct {
	// resources maps an absolute base URI to the Node that declared it (the document
	// root, or a node carrying an `$id`).
	resources map[string]*Node
	// pointers maps "<baseURI>\x00<json-pointer>" to the Node found by navigating the
	// resource identified by baseURI along that (resource-relative) JSON Pointer.
	pointers map[string]*Node
	// anchors maps "<baseURI>#<anchor>" to the Node carrying that `$anchor`.
	anchors map[string]*Node
	// dynAnchors maps "<baseURI>#<anchor>" to the Node carrying that `$dynamicAnchor`.
	dynAnchors map[string]*Node

	// nodes lists every Node discovered during conversion, in discovery order. Used for
	// reference-graph construction (scc.go).
	nodes []*Node
	// edges is the reference/applicator graph: outgoing edges per node.
	edges map[*Node][]edge

	// sccs holds every strongly-connected-component containing more than one node, or a
	// single node with a self-loop (i.e. every *recursive* SCC).
	sccs []SCC
	// sccIndex maps a Node to its index within sccs, for nodes that participate in a
	// recursive SCC only.
	sccIndex map[*Node]int

	// hasDynamicRefs reports whether any node in the document used $dynamicRef.
	hasDynamicRefs bool
}

// edge is one outgoing reference/applicator edge in the schema graph.
type edge struct {
	to      *Node
	descent bool // true if this edge represents instance-descent (design §19)
}

func newRegistry() *Registry {
	return &Registry{
		resources:  make(map[string]*Node),
		pointers:   make(map[string]*Node),
		anchors:    make(map[string]*Node),
		dynAnchors: make(map[string]*Node),
		edges:      make(map[*Node][]edge),
		sccIndex:   make(map[*Node]int),
	}
}

// Resource returns the resource root Node registered under the given absolute base URI
// (the document root, or a subtree carrying `$id`), and whether it was found.
func (r *Registry) Resource(baseURI string) (*Node, bool) {
	n, ok := r.resources[baseURI]
	return n, ok
}

// Anchor returns the Node carrying `$anchor == anchor` within the resource identified by
// baseURI, and whether it was found.
func (r *Registry) Anchor(baseURI, anchor string) (*Node, bool) {
	n, ok := r.anchors[baseURI+"#"+anchor]
	return n, ok
}

// DynamicAnchor returns the Node carrying `$dynamicAnchor == anchor` within the resource
// identified by baseURI, and whether it was found.
func (r *Registry) DynamicAnchor(baseURI, anchor string) (*Node, bool) {
	n, ok := r.dynAnchors[baseURI+"#"+anchor]
	return n, ok
}

// HasDynamicRefs reports whether the loaded document uses `$dynamicRef` anywhere. Their
// runtime (dynamic-scope) resolution is left to later phases; Phase 1 only records their
// presence and exposes the anchor graph via DynamicAnchor.
func (r *Registry) HasDynamicRefs() bool {
	return r.hasDynamicRefs
}

// RecursionClass classifies a schema's recursion, as determined by the reference-graph
// SCC analysis (design §19).
type RecursionClass int

const (
	// NotRecursive means the node is not part of any reference cycle.
	NotRecursive RecursionClass = iota
	// Guarded means the node is part of a cycle, but every cycle through its SCC
	// crosses at least one instance-descent edge (object property / array item), so a
	// recursive Go type can normally be generated.
	Guarded
	// Unguarded means the node is part of a cycle that never crosses an
	// instance-descent edge (e.g. a pure allOf/$ref loop) — semantically recursive
	// without a structural base case.
	Unguarded
)

// String implements fmt.Stringer.
func (c RecursionClass) String() string {
	switch c {
	case NotRecursive:
		return "not-recursive"
	case Guarded:
		return "guarded"
	case Unguarded:
		return "unguarded"
	default:
		return "unknown"
	}
}

// SCC is one strongly-connected-component of the reference graph containing a genuine
// cycle (size > 1, or a single node with a self-loop).
type SCC struct {
	Nodes []*Node
	Class RecursionClass
}

// ClassifyRecursion returns the [RecursionClass] of n, i.e. whether it participates in a
// recursive SCC and, if so, whether that SCC is guarded or unguarded (design §19).
func (r *Registry) ClassifyRecursion(n *Node) RecursionClass {
	i, ok := r.sccIndex[n]
	if !ok {
		return NotRecursive
	}
	return r.sccs[i].Class
}

// SCCs returns every recursive strongly-connected-component discovered in the reference
// graph.
func (r *Registry) SCCs() []SCC {
	return r.sccs
}

// RefTargets returns every Node that is the resolved target of a static `$ref`, keyed by
// its SchemaID (the target's [Node.Pointer], the same key used for recursion
// classification and for ir.Ref.Target). These are exactly the schemas that must become
// named definitions in a whole-document plan (design §10.1); unresolved and $dynamicRef
// targets are excluded.
func (r *Registry) RefTargets() map[string]*Node {
	out := make(map[string]*Node)
	for _, n := range r.nodes {
		if n.Resolved != nil {
			out[n.Resolved.Pointer] = n.Resolved
		}
	}
	return out
}
