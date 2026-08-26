package scc_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/scc"
)

// render sorts each component and the components themselves, so a test asserts membership
// rather than the discovery order Tarjan happens to produce.
func render(comps [][]string) string {
	out := make([]string, len(comps))
	for i, c := range comps {
		c = slices.Clone(c)
		slices.Sort(c)
		out[i] = strings.Join(c, "")
	}
	slices.Sort(out)
	return strings.Join(out, " ")
}

func TestComponents(t *testing.T) {
	tests := []struct {
		name  string
		nodes string
		edges map[string]string
		want  string
	}{
		{"isolated nodes", "abc", nil, "a b c"},
		{"a chain", "abc", map[string]string{"a": "b", "b": "c"}, "a b c"},
		{"a two-cycle", "ab", map[string]string{"a": "b", "b": "a"}, "ab"},
		{"a self-loop", "a", map[string]string{"a": "a"}, "a"},
		{"a three-cycle", "abc", map[string]string{"a": "b", "b": "c", "c": "a"}, "abc"},
		{"a cycle with a tail", "abcd", map[string]string{"a": "b", "b": "a", "c": "a", "d": "c"}, "ab c d"},
		{"two disjoint cycles", "abcd", map[string]string{"a": "b", "b": "a", "c": "d", "d": "c"}, "ab cd"},
		{"a node reachable but not listed", "a", map[string]string{"a": "b"}, "a b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := func(n string) []string { return strings.Split(tt.edges[n], "") }
			nodes := strings.Split(tt.nodes, "")
			require.Equal(t, tt.want, render(scc.Components(nodes, edges)))
		})
	}
}

func TestRecursive(t *testing.T) {
	edges := func(n string) []string {
		if n == "loop" {
			return []string{"loop"}
		}
		return nil
	}
	require.True(t, scc.Recursive([]string{"loop"}, edges), "a self-loop is a cycle")
	require.False(t, scc.Recursive([]string{"plain"}, edges), "a lone node is not")
	require.True(t, scc.Recursive([]string{"a", "b"}, edges), "a multi-node component always is")
}

// TestComponentsIsOrderStable pins the property both callers depend on: the same node
// order in gives the same components out, so a lowering that walks them is reproducible.
func TestComponentsIsOrderStable(t *testing.T) {
	nodes := strings.Split("abcdef", "")
	adj := map[string]string{"a": "b", "b": "ca", "c": "a", "d": "e", "e": "d", "f": "af"}
	edges := func(n string) []string { return strings.Split(adj[n], "") }

	want := scc.Components(nodes, edges)
	for range 32 {
		require.Equal(t, want, scc.Components(nodes, edges))
	}
}
