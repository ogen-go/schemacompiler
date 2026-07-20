package frontend

// analyzeInhabitation computes, by least fixed point over the reference graph, which nodes
// are inhabited — have at least one finite JSON instance — and records the recursive nodes
// that are not (design §8, issue #8).
//
// The SCC pass (scc.go) classifies recursion by termination: a required self-recursion such
// as `A: {type:object, required:[self], properties:{self:{$ref:A}}}` is Guarded, because the
// `properties.self` descent edge means a base case *could* exist in the emitted Go type. But
// no JSON instance ever reaches that base case — every instance must contain a strictly
// smaller instance of itself — so the schema is uninhabited. Termination and inhabitation are
// orthogonal, and this pass supplies the latter.
//
// The analysis is a sound over-approximation of inhabitation: any construct it does not model
// (combinators, conditionals, const/enum, arrays) makes the node assumed inhabited, so a
// satisfiable schema is never wrongly flagged (only false negatives, never false positives).
// A node is proven inhabited when a finite witness can be built from already-inhabited parts;
// a purely-required cycle never produces one, so it stays uninhabited at the fixed point.
//
// Only recursive, required-object emptiness is reported — the surprising case the SCC pass
// misses. Local emptiness (a `false` schema, disjoint kinds, const conflicts) is already
// modeled by normalization and is intentionally not reported here.
func (r *Registry) analyzeInhabitation() {
	inhabited := make(map[*Node]bool, len(r.nodes))
	for {
		changed := false
		for _, n := range r.nodes {
			if inhabited[n] {
				continue
			}
			if r.nodeInhabited(n, inhabited) {
				inhabited[n] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	for _, n := range r.nodes {
		if inhabited[n] {
			continue
		}
		if _, recursive := r.sccIndex[n]; recursive && len(n.Required) > 0 {
			r.uninhabited = append(r.uninhabited, UninhabitedNode{
				Pointer: n.Pointer,
				Reason:  "object requires a property whose schema recursively requires itself; no finite instance exists",
			})
		}
	}
}

// nodeInhabited reports whether n has a finite JSON instance, given the inhabitation already
// proven for other nodes. It over-approximates: an unmodeled construct yields true, so the
// result is only ever used to prove emptiness, never to assume it.
func (r *Registry) nodeInhabited(n *Node, inhabited map[*Node]bool) bool {
	if n.Always != nil {
		return *n.Always
	}
	// A $ref is a conjunctive constraint: an instance must also satisfy the target, so a
	// provably-uninhabited target makes this node uninhabited too.
	if n.Ref != "" && n.Resolved != nil && !inhabited[n.Resolved] {
		return false
	}
	// Constructs not modeled here are assumed inhabited (sound: never a false positive).
	if len(n.AllOf) > 0 || len(n.AnyOf) > 0 || len(n.OneOf) > 0 ||
		n.Not != nil || n.If != nil || n.Const != nil || len(n.Enum) > 0 {
		return true
	}
	// `required` only constrains object instances. If a non-object kind is permitted (or no
	// `type` restricts the kind), a scalar/array/null witness satisfies the schema
	// regardless of `required`, so the node is inhabited.
	if !n.HasType || n.Types != KindObject {
		return true
	}
	// Object-only: inhabited iff every required property's schema is inhabited.
	for _, name := range n.Required {
		sp := propertySchema(n, name)
		if sp == nil {
			// A required name with no property schema is satisfiable by any value unless
			// additionalProperties forbids adding it.
			if ap := n.AdditionalProperties; ap != nil && ap.Always != nil && !*ap.Always {
				return false
			}
			continue
		}
		if !inhabited[sp] {
			return false
		}
	}
	return true
}

// propertySchema returns the schema declared for property name, or nil if none.
func propertySchema(n *Node, name string) *Node {
	for _, p := range n.Properties {
		if p.Name == name {
			return p.Schema
		}
	}
	return nil
}
