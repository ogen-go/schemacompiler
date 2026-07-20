package frontend

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DOT renders the schema's reference graph as Graphviz DOT source (design §10, §19), for
// debugging reference resolution and recursion classification. Output is deterministic:
// nodes and edges are emitted in Registry discovery order.
func (s *Schema) DOT() string {
	var sb strings.Builder
	s.WriteDOT(&sb)
	return sb.String()
}

// WriteDOT writes the Graphviz DOT source for the schema's reference graph to w.
//
// One graph node is emitted per [*Node] in the registry, de-duplicated by pointer
// identity. Edges are solid for instance-descent (object property / array item) and
// dashed otherwise (e.g. allOf/$ref); a `$ref` edge is additionally labeled. Nodes
// belonging to a recursive strongly-connected-component are colored green (guarded) or
// red (unguarded), per [Registry.ClassifyRecursion].
func (s *Schema) WriteDOT(w io.Writer) {
	reg := s.Registry
	if reg == nil {
		_, _ = fmt.Fprintln(w, "digraph schema {}")
		return
	}

	ids := make(map[*Node]string, len(reg.nodes))
	for i, n := range reg.nodes {
		ids[n] = fmt.Sprintf("n%d", i)
	}

	_, _ = fmt.Fprintln(w, "digraph schema {")
	_, _ = fmt.Fprintln(w, `  // legend: solid edge = instance descent, dashed edge = non-descent (e.g. $ref/allOf)`)
	_, _ = fmt.Fprintln(w, `  // legend: green node = guarded recursive SCC, red node = unguarded recursive SCC`)
	_, _ = fmt.Fprintln(w, "  rankdir=LR;")

	for _, n := range reg.nodes {
		_, _ = fmt.Fprintf(w, "  %s [label=%s%s];\n", ids[n], strconv.Quote(nodeLabel(n)), nodeColorAttr(reg, n))
	}

	for _, n := range reg.nodes {
		for _, e := range reg.edges[n] {
			if e.to == nil {
				continue
			}
			style := "dashed"
			if e.descent {
				style = "solid"
			}
			label := ""
			if n.Ref != "" && e.to == n.Resolved {
				label = ` label="$ref"`
			}
			_, _ = fmt.Fprintf(w, "  %s -> %s [style=%s%s];\n", ids[n], ids[e.to], style, label)
		}
	}

	_, _ = fmt.Fprintln(w, "}")
}

// nodeLabel picks a short, stable display label for n: its JSON Pointer, or "#"+Anchor /
// $id when present and the pointer is empty (the document root), falling back to a
// generic marker for boolean schemas.
func nodeLabel(n *Node) string {
	if n.Always != nil {
		return fmt.Sprintf("bool(%v)", *n.Always)
	}
	switch {
	case n.ID != "":
		return "$id:" + n.ID
	case n.Anchor != "":
		return "#" + n.Anchor
	case n.Pointer != "":
		return n.Pointer
	default:
		return "#"
	}
}

// nodeColorAttr returns a trailing DOT attribute fragment coloring n by its recursion
// classification, or "" when n is not part of any recursive SCC.
func nodeColorAttr(reg *Registry, n *Node) string {
	switch reg.ClassifyRecursion(n) {
	case Guarded:
		return `, style=filled, fillcolor=green`
	case Unguarded:
		return `, style=filled, fillcolor=red`
	default:
		return ""
	}
}
