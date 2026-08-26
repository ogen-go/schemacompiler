package frontend

import (
	"github.com/ogen-go/schemacompiler/internal/scc"
)

// analyzeSCCs finds the strongly connected components of the reference graph accumulated
// in r.nodes/r.edges, then classifies every recursive one as guarded or unguarded per
// design §19: a recursive SCC is guarded if every cycle within it crosses at least one
// instance-descent edge (object property / array item traversal).
func (r *Registry) analyzeSCCs() {
	edges := r.successors()
	for _, comp := range scc.Components(r.nodes, edges) {
		if !scc.Recursive(comp, edges) {
			continue
		}
		idx := len(r.sccs)
		r.sccs = append(r.sccs, SCC{Nodes: comp, Class: r.classify(comp)})
		for _, n := range comp {
			r.sccIndex[n] = idx
		}
	}
}

func (r *Registry) successors() func(*Node) []*Node {
	return func(n *Node) []*Node {
		out := make([]*Node, 0, len(r.edges[n]))
		for _, e := range r.edges[n] {
			if e.to != nil {
				out = append(out, e.to)
			}
		}
		return out
	}
}

// classify determines whether every cycle within comp crosses a descent edge. Removing
// the descent edges from the SCC's induced subgraph answers it: if what remains is still
// cyclic, some cycle never crosses a descent edge and the SCC is unguarded.
func (r *Registry) classify(comp []*Node) RecursionClass {
	inSCC := make(map[*Node]bool, len(comp))
	for _, n := range comp {
		inSCC[n] = true
	}
	nonDescent := func(n *Node) []*Node {
		var out []*Node
		for _, e := range r.edges[n] {
			if !e.descent && e.to != nil && inSCC[e.to] {
				out = append(out, e.to)
			}
		}
		return out
	}

	for _, c := range scc.Components(comp, nonDescent) {
		if scc.Recursive(c, nonDescent) {
			return Unguarded
		}
	}
	return Guarded
}
