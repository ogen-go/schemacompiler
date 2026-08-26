package gogen

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/ogen-go/schemacompiler/internal/scc"
)

// directSlots applies f to every type stored *inline* in t, writing the result back.
//
// Inline is what makes a Go type cycle a compile error, so it is also what the recursion
// pass has to look at. A slice, a map and an interface all hold their element behind an
// indirection the language already provides, so they end the walk; a struct field, a tuple
// slot and an [opt] wrapper store the value in the containing type's memory and continue
// it. [Named] is a boundary in both directions: reaching one is an edge, and its
// Underlying is the only slot it has.
func directSlots(t GoType, f func(GoType) GoType) {
	switch t := t.(type) {
	case *Named:
		t.Underlying = f(t.Underlying)
	case *Struct:
		for i := range t.Fields {
			t.Fields[i].Type = f(t.Fields[i].Type)
		}
	case *Tuple:
		for i := range t.Elems {
			t.Elems[i] = f(t.Elems[i])
		}
	case *Presence:
		t.Elem = f(t.Elem)
	case *Primitive, *Any, *Never, *Slice, *Map, *Interface, *Pointer:
	default:
		panic(fmt.Sprintf("gogen: unhandled GoType variant %T", t))
	}
}

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

	seen := make(map[GoType]bool, len(types))
	for _, n := range types {
		pointerize(n, seen)
	}
}

// directRefs is the named types n stores inline, sorted so the component walk is
// reproducible.
func directRefs(n *Named) []*Named {
	var out []*Named
	seen := make(map[*Named]bool)
	var walk func(GoType)
	walk = func(t GoType) {
		if ref, ok := t.(*Named); ok {
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
			return
		}
		directSlots(t, func(c GoType) GoType {
			walk(c)
			return c
		})
	}
	walk(n.Underlying)

	slices.SortFunc(out, func(a, b *Named) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func pointerize(t GoType, seen map[GoType]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	directSlots(t, func(c GoType) GoType {
		if ref, ok := c.(*Named); ok {
			if ref.Recursive {
				return &Pointer{Elem: ref}
			}
			return c
		}
		pointerize(c, seen)
		return c
	})
}
