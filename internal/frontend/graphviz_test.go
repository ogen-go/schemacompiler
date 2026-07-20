package frontend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchema_DOT_RecursiveGuarded(t *testing.T) {
	data := []byte(`{
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {
					"value": {"type": "number"},
					"next": {"$ref": "#/$defs/Node"}
				}
			}
		},
		"$ref": "#/$defs/Node"
	}`)

	s, err := Load(context.Background(), data, "")
	require.NoError(t, err)

	got := s.DOT()
	require.Contains(t, got, "digraph schema {")

	// One node per registry entry, including the root $ref node and the $defs/Node
	// target it resolves to.
	require.NotEmpty(t, s.Registry.nodes)

	require.Contains(t, got, `label="/$defs/Node"`)
	require.Contains(t, got, `label="$ref"`)
	require.Contains(t, got, "style=dashed")
	require.Contains(t, got, "style=solid")

	// The Node schema is a guarded recursive SCC: the cycle Node -> next($ref) -> Node
	// crosses the "next" property's instance-descent edge.
	require.Contains(t, got, "fillcolor=green")
	require.NotContains(t, got, "fillcolor=red")
}
