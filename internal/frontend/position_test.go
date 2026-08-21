package frontend

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func TestPositions(t *testing.T) {
	const doc = `type: object
properties:
  name:
    type: string
  other:
    $ref: '#/$defs/Missing'
additionalProperties: false
`
	s, err := Load(context.Background(), []byte(doc), "schema.yml")
	require.NoError(t, err)
	require.Equal(t, plan.Position{File: "schema.yml", Line: 1, Column: 1}, s.Root.Position)

	byName := make(map[string]*Node, len(s.Root.Properties))
	for _, p := range s.Root.Properties {
		byName[p.Name] = p.Schema
	}
	require.Equal(t, plan.Position{File: "schema.yml", Line: 4, Column: 5}, byName["name"].Position)
	require.Equal(t, plan.Position{File: "schema.yml", Line: 6, Column: 5}, byName["other"].Position)
	require.Equal(t, plan.Position{File: "schema.yml", Line: 7, Column: 23}, s.Root.AdditionalProperties.Position)

	require.Len(t, s.Unresolved, 1)
	require.Equal(t, byName["other"].Position, s.Unresolved[0].Position)

	pos, ok := s.Registry.PositionOf("/properties/name")
	require.True(t, ok)
	require.Equal(t, byName["name"].Position, pos)
}

func TestPositionsExternalDocument(t *testing.T) {
	const root = `{"$ref": "other.json#/$defs/Name"}`
	other := []byte("{\n  \"$defs\": {\n    \"Name\": {\"type\": \"string\"}\n  }\n}")
	loader := func(context.Context, *url.URL) ([]byte, error) { return other, nil }

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", loader)
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)
	require.NotNil(t, s.Root.Resolved)
	require.Equal(t,
		plan.Position{File: "https://ex.com/other.json", Line: 3, Column: 13},
		s.Root.Resolved.Position)
}

func TestPositionsBooleanDocument(t *testing.T) {
	s, err := Load(context.Background(), []byte("true"), "bool.json")
	require.NoError(t, err)
	require.Equal(t, plan.Position{File: "bool.json", Line: 1, Column: 1}, s.Root.Position)
}
