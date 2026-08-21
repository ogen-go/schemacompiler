package ir

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestMetadataOf(t *testing.T) {
	tests := []struct {
		name string
		node *frontend.Node
		want plan.Metadata
	}{
		{name: "nil", node: nil, want: plan.Metadata{}},
		{name: "empty", node: &frontend.Node{}, want: plan.Metadata{}},
		{
			name: "annotations",
			node: &frontend.Node{
				Title:       "T",
				Description: "D",
				Deprecated:  true,
				ReadOnly:    true,
				WriteOnly:   true,
				Default:     &frontend.Value{Raw: []byte(`1`)},
				Examples:    []frontend.Value{{Raw: []byte(`"a"`)}},
				XML:         &frontend.XML{Name: "N", Namespace: "ns", Prefix: "p", Attribute: true, Wrapped: true},
				Extensions:  map[string]any{"x-k": "v"},
			},
			want: plan.Metadata{
				Title:       "T",
				Description: "D",
				Deprecated:  true,
				ReadOnly:    true,
				WriteOnly:   true,
				Default:     []byte(`1`),
				Examples:    [][]byte{[]byte(`"a"`)},
				XML:         &plan.XMLMetadata{Name: "N", Namespace: "ns", Prefix: "p", Attribute: true, Wrapped: true},
				Extensions:  map[string]any{"x-k": "v"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, MetadataOf(tt.node))
		})
	}
}
