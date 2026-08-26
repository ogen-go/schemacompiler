package gogen

import (
	"cmp"
	"slices"

	"github.com/ogen-go/schemacompiler/internal/scc"
)

// breakCycles marks every type that inline storage makes cyclic, then rewrites each direct
// reference to a marked type as a [Pointer].
//
// The decision is per node, never per edge. Choosing edges to cut is the feedback arc set
// problem — NP-hard, and with no canonical answer, so two documents referencing the same
// schema could cut differently and disagree about its shape. Choosing nodes has one
// answer: SCC membership.
func breakCycles(types []*Named) {
	edges := make(map[*Named][]*Named, len(types))
	for _, n := range types {
		edges[n] = directRefs(n)
	}
	succ := func(n *Named) []*Named { return edges[n] }

	for _, comp := range scc.Components(types, succ) {
		if !scc.Recursive(comp, succ) {
			continue
		}
		for _, n := range comp {
			n.Recursive = true
		}
	}

	for _, n := range types {
		pointerize(n)
	}
}

// directRefs is the named types n stores inline, sorted so the component walk is
// reproducible.
//
// Inline is what makes a Go type cycle a compile error, so it is what the recursion pass
// looks at: an edge the language already indirects through cannot close one. [Edge.Indirect]
// is where that rule lives, so this is a walk that stops at indirect edges and at any
// [Named], which is an edge rather than something to descend into.
func directRefs(n *Named) []*Named {
	var out []*Named
	seen := make(map[*Named]bool)
	Fold(n.Underlying, struct{}{}, func(acc struct{}, node Node) (struct{}, Action) {
		if node.Edge.Indirect {
			return acc, Skip
		}
		ref, ok := node.Type.(*Named)
		if !ok {
			return acc, Descend
		}
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
		return acc, Skip
	})

	slices.SortFunc(out, func(a, b *Named) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// pointerize rewrites every inline reference to a recursive type as a pointer to it.
func pointerize(t GoType) {
	seen := make(map[GoType]bool)
	var walk func(GoType)
	walk = func(t GoType) {
		if seen[t] {
			return
		}
		seen[t] = true
		apply(t, func(n Node) GoType {
			if ref, ok := n.Type.(*Named); ok {
				if ref.Recursive && !n.Edge.Indirect {
					return &Pointer{Elem: ref}
				}
				return n.Type
			}
			walk(n.Type)
			return n.Type
		})
	}
	walk(t)
}
