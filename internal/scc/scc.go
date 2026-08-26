// Package scc computes the strongly connected components of a directed graph.
//
// It exists because two passes need the same answer over different graphs: the frontend
// classifies recursive `$ref` cycles over instance-descent edges (design §19), and the Go
// backend decides pointer indirection over Go-storage edges. The graphs disagree — a
// `oneOf` alternative is a Go interface but not an instance descent — so what they share
// is the algorithm, not the edge set.
package scc

// Components returns the strongly connected components of the graph over nodes, with
// adjacency given by edges, in reverse topological order (Tarjan).
//
// Nodes reachable through edges but absent from nodes are still visited; nodes lists the
// roots the walk starts from and fixes the order components are discovered in, which is
// what makes the result reproducible.
func Components[T comparable](nodes []T, edges func(T) []T) [][]T {
	t := &tarjan[T]{
		index:   make(map[T]int, len(nodes)),
		lowlink: make(map[T]int, len(nodes)),
		onStack: make(map[T]bool, len(nodes)),
		edges:   edges,
	}
	for _, n := range nodes {
		if _, seen := t.index[n]; !seen {
			t.strongconnect(n)
		}
	}
	return t.components
}

// Recursive reports whether comp is a cycle. Every multi-node component is one; a
// single-node component is only a cycle if the node has a self-loop, since Tarjan reports
// every node as a component of its own.
func Recursive[T comparable](comp []T, edges func(T) []T) bool {
	if len(comp) != 1 {
		return len(comp) > 1
	}
	n := comp[0]
	for _, m := range edges(n) {
		if m == n {
			return true
		}
	}
	return false
}

type tarjan[T comparable] struct {
	counter    int
	index      map[T]int
	lowlink    map[T]int
	onStack    map[T]bool
	stack      []T
	edges      func(T) []T
	components [][]T
}

func (t *tarjan[T]) strongconnect(v T) {
	t.index[v] = t.counter
	t.lowlink[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.edges(v) {
		if _, seen := t.index[w]; !seen {
			t.strongconnect(w)
			if t.lowlink[w] < t.lowlink[v] {
				t.lowlink[v] = t.lowlink[w]
			}
		} else if t.onStack[w] {
			if t.index[w] < t.lowlink[v] {
				t.lowlink[v] = t.index[w]
			}
		}
	}

	if t.lowlink[v] != t.index[v] {
		return
	}
	var comp []T
	for {
		i := len(t.stack) - 1
		w := t.stack[i]
		t.stack = t.stack[:i]
		t.onStack[w] = false
		comp = append(comp, w)
		if w == v {
			break
		}
	}
	t.components = append(t.components, comp)
}
