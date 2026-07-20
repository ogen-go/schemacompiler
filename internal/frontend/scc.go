package frontend

// analyzeSCCs runs Tarjan's strongly-connected-component algorithm over the reference
// graph accumulated in r.nodes/r.edges, then classifies every recursive SCC as guarded or
// unguarded per design §19: a recursive SCC is guarded iff every cycle within it crosses
// at least one instance-descent edge (object property / array item traversal).
//
// The classification is computed by removing descent edges from the SCC's induced
// subgraph: if what remains is still cyclic, some cycle never crosses a descent edge, so
// the SCC is unguarded; otherwise every cycle must cross a descent edge, so it is guarded.
func (r *Registry) analyzeSCCs() {
	t := &tarjan{
		index:   make(map[*Node]int),
		lowlink: make(map[*Node]int),
		onStack: make(map[*Node]bool),
	}
	for _, n := range r.nodes {
		if _, seen := t.index[n]; !seen {
			t.strongconnect(n, r)
		}
	}

	for _, comp := range t.components {
		recursive := len(comp) > 1
		if !recursive {
			// A single-node component is only "recursive" if it has a self-loop.
			n := comp[0]
			for _, e := range r.edges[n] {
				if e.to == n {
					recursive = true
					break
				}
			}
		}
		if !recursive {
			continue
		}
		class := classifySCC(comp, r.edges)
		idx := len(r.sccs)
		r.sccs = append(r.sccs, SCC{Nodes: comp, Class: class})
		for _, n := range comp {
			r.sccIndex[n] = idx
		}
	}
}

// classifySCC determines whether every cycle within comp crosses a descent edge.
func classifySCC(comp []*Node, edges map[*Node][]edge) RecursionClass {
	inSCC := make(map[*Node]bool, len(comp))
	for _, n := range comp {
		inSCC[n] = true
	}

	// Build the induced subgraph restricted to non-descent edges, then check it for a
	// cycle using a second, independent Tarjan pass.
	nonDescent := make(map[*Node][]*Node)
	for _, n := range comp {
		for _, e := range edges[n] {
			if !e.descent && inSCC[e.to] {
				nonDescent[n] = append(nonDescent[n], e.to)
			}
		}
	}

	if hasCycle(comp, nonDescent) {
		return Unguarded
	}
	return Guarded
}

// hasCycle reports whether the graph (restricted to nodes, with adjacency adj) contains
// any cycle, including a self-loop.
func hasCycle(nodes []*Node, adj map[*Node][]*Node) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[*Node]int, len(nodes))
	var visit func(n *Node) bool
	visit = func(n *Node) bool {
		color[n] = gray
		for _, m := range adj[n] {
			switch color[m] {
			case gray:
				return true
			case white:
				if visit(m) {
					return true
				}
			}
		}
		color[n] = black
		return false
	}
	for _, n := range nodes {
		if color[n] == white {
			if visit(n) {
				return true
			}
		}
	}
	return false
}

// tarjan implements Tarjan's SCC algorithm iteratively-recursive over *Node via
// registry-provided edges.
type tarjan struct {
	counter    int
	index      map[*Node]int
	lowlink    map[*Node]int
	onStack    map[*Node]bool
	stack      []*Node
	components [][]*Node
}

func (t *tarjan) strongconnect(v *Node, r *Registry) {
	t.index[v] = t.counter
	t.lowlink[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, e := range r.edges[v] {
		w := e.to
		if w == nil {
			continue
		}
		if _, seen := t.index[w]; !seen {
			t.strongconnect(w, r)
			if t.lowlink[w] < t.lowlink[v] {
				t.lowlink[v] = t.lowlink[w]
			}
		} else if t.onStack[w] {
			if t.index[w] < t.lowlink[v] {
				t.lowlink[v] = t.index[w]
			}
		}
	}

	if t.lowlink[v] == t.index[v] {
		var comp []*Node
		for {
			n := len(t.stack) - 1
			w := t.stack[n]
			t.stack = t.stack[:n]
			t.onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		t.components = append(t.components, comp)
	}
}
