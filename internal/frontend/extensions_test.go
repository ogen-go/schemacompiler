package frontend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_Extensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want map[string]any
	}{
		{"none", `{"type": "string"}`, nil},
		{"scalar", `{"x-ogen-name": "Foo"}`, map[string]any{"x-ogen-name": "Foo"}},
		{"number", `{"x-n": 3}`, map[string]any{"x-n": 3}},
		{"bool", `{"x-b": true}`, map[string]any{"x-b": true}},
		{"null", `{"x-null": null}`, map[string]any{"x-null": nil}},
		{
			"nested",
			`{"x-ogen-properties": {"a": {"name": "B"}}, "x-list": [1, "s"]}`,
			map[string]any{
				"x-ogen-properties": map[string]any{"a": map[string]any{"name": "B"}},
				"x-list":            []any{1, "s"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustLoad(t, tc.doc)
			require.Equal(t, tc.want, s.Root.Extensions)
		})
	}
}

func TestLoad_PropertyExtensions(t *testing.T) {
	s := mustLoad(t, `{
		"type": "object",
		"properties": {
			"a": {"type": "string", "x-ogen-type": "time.Time"}
		}
	}`)
	require.Len(t, s.Root.Properties, 1)
	require.Equal(t, map[string]any{"x-ogen-type": "time.Time"}, s.Root.Properties[0].Schema.Extensions)
}

func TestLoad_XML(t *testing.T) {
	s := mustLoad(t, `{"type": "string", "xml": {"name": "Tag", "namespace": "urn:x", "prefix": "p", "attribute": true, "wrapped": true}}`)
	require.Equal(t, &XML{
		Name:      "Tag",
		Namespace: "urn:x",
		Prefix:    "p",
		Attribute: true,
		Wrapped:   true,
	}, s.Root.XML)
}
