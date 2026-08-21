package frontend

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

func TestStripRefs(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		// want maps a JSON Pointer (of the schema node the `$ref` was taken from) to the
		// reference string; a nil want means nothing may be stripped.
		want map[string]string
	}{
		{
			name: "root",
			doc:  `{"$ref": "#/$defs/A"}`,
			want: map[string]string{"": "#/$defs/A"},
		},
		{
			name: "sibling keywords",
			doc:  `{"$ref": "#/$defs/A", "type": "string"}`,
			want: map[string]string{"": "#/$defs/A"},
		},
		{
			name: "property named $ref",
			doc:  `{"properties": {"$ref": {"type": "string"}}}`,
			want: nil,
		},
		{
			name: "property named $ref containing an actual $ref",
			doc:  `{"properties": {"$ref": {"$ref": "#/$defs/A"}}}`,
			want: map[string]string{"/properties/$ref": "#/$defs/A"},
		},
		{
			name: "pattern named $ref",
			doc:  `{"patternProperties": {"$ref": {"type": "string"}}}`,
			want: nil,
		},
		{
			name: "definition named $ref",
			doc:  `{"$defs": {"$ref": {"type": "string"}}}`,
			want: nil,
		},
		{
			name: "dependent schema named $ref",
			doc:  `{"dependentSchemas": {"$ref": {"type": "string"}}}`,
			want: nil,
		},
		{
			name: "const value",
			doc:  `{"const": {"$ref": "#/$defs/A"}}`,
			want: nil,
		},
		{
			name: "enum value",
			doc:  `{"enum": [{"$ref": "#/$defs/A"}]}`,
			want: nil,
		},
		{
			name: "nested enum value",
			doc:  `{"enum": [{"a": [{"$ref": "#/$defs/A"}]}]}`,
			want: nil,
		},
		{
			name: "default value",
			doc:  `{"default": {"$ref": "#/$defs/A"}}`,
			want: nil,
		},
		{
			name: "examples value",
			doc:  `{"examples": [{"$ref": "#/$defs/A"}]}`,
			want: nil,
		},
		{
			name: "extension value",
			doc:  `{"x-vendor": {"$ref": "#/$defs/A"}}`,
			want: nil,
		},
		{
			name: "unknown keyword value",
			doc:  `{"definitions": {"A": {"$ref": "#/$defs/A"}}}`,
			want: nil,
		},
		{
			name: "applicator list",
			doc:  `{"allOf": [{"$ref": "#/$defs/A"}], "anyOf": [{"$ref": "#/$defs/B"}]}`,
			want: map[string]string{"/allOf/0": "#/$defs/A", "/anyOf/0": "#/$defs/B"},
		},
		{
			name: "single schema keywords",
			doc:  `{"not": {"$ref": "#/$defs/A"}, "items": {"$ref": "#/$defs/B"}, "propertyNames": {"$ref": "#/$defs/C"}}`,
			want: map[string]string{"/not": "#/$defs/A", "/items": "#/$defs/B", "/propertyNames": "#/$defs/C"},
		},
		{
			name: "schema map values",
			doc:  `{"$defs": {"A": {"$ref": "#/$defs/B"}}, "dependentSchemas": {"a": {"$ref": "#/$defs/C"}}}`,
			want: map[string]string{"/$defs/A": "#/$defs/B", "/dependentSchemas/a": "#/$defs/C"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := loadDocument([]byte(tt.doc))
			require.NoError(t, err)

			refs := make(map[*yaml.Node]string)
			stripRefs(root, refs)

			byPointer := make(map[string]string, len(refs))
			for node, ref := range refs {
				byPointer[nodePointer(t, root, "", node)] = ref
			}
			require.Equal(t, tt.want, emptyToNil(byPointer))
		})
	}
}

func emptyToNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

// nodePointer finds target within root and returns its JSON Pointer, failing the test when
// the node is not reachable.
func nodePointer(t *testing.T, root *yaml.Node, pointer string, target *yaml.Node) string {
	t.Helper()
	if found, ok := findNodePointer(root, pointer, target); ok {
		return found
	}
	t.Fatalf("node stripped of $ref is not reachable from the document root")
	return ""
}

func findNodePointer(n *yaml.Node, pointer string, target *yaml.Node) (string, bool) {
	if n == target {
		return pointer, true
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if p, ok := findNodePointer(n.Content[i+1], jsonPointerAppend(pointer, n.Content[i].Value), target); ok {
				return p, true
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for i, c := range n.Content {
			if p, ok := findNodePointer(c, jsonPointerAppend(pointer, strconv.Itoa(i)), target); ok {
				return p, true
			}
		}
	}
	return "", false
}

// TestLoadRefAsData pins the frontend AST for a `$ref` written where the document holds
// instance data: it must survive as data rather than being consumed as a reference.
func TestLoadRefAsData(t *testing.T) {
	ctx := context.Background()

	t.Run("property named $ref", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"properties": {"$ref": {"type": "string"}}}`), "")
		require.NoError(t, err)
		require.Empty(t, s.Root.Ref)
		require.Len(t, s.Root.Properties, 1)
		require.Equal(t, "$ref", s.Root.Properties[0].Name)
		require.Equal(t, KindString, s.Root.Properties[0].Schema.Types)
	})

	t.Run("property named $ref containing an actual $ref", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"$defs": {"A": {"type": "string"}}, "properties": {"$ref": {"$ref": "#/$defs/A"}}}`), "")
		require.NoError(t, err)
		require.Empty(t, s.Root.Ref)
		require.Len(t, s.Root.Properties, 1)
		field := s.Root.Properties[0]
		require.Equal(t, "$ref", field.Name)
		require.Equal(t, "#/$defs/A", field.Schema.Ref)
		require.NotNil(t, field.Schema.Resolved)
		require.Equal(t, KindString, field.Schema.Resolved.Types)
	})

	t.Run("pattern named $ref", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"patternProperties": {"$ref": {"type": "string"}}}`), "")
		require.NoError(t, err)
		require.Empty(t, s.Root.Ref)
		require.Len(t, s.Root.PatternProperties, 1)
		require.Equal(t, "$ref", s.Root.PatternProperties[0].Name)
	})

	t.Run("dependent schema named $ref", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"dependentSchemas": {"$ref": {"required": ["a"]}}}`), "")
		require.NoError(t, err)
		require.Empty(t, s.Root.Ref)
		require.Len(t, s.Root.DependentSchemas, 1)
		require.Equal(t, "$ref", s.Root.DependentSchemas[0].Name)
	})

	t.Run("definition named $ref", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"$defs": {"$ref": {"type": "string"}}, "$ref": "#/$defs/$ref"}`), "")
		require.NoError(t, err)
		require.Equal(t, "#/$defs/$ref", s.Root.Ref)
		require.NotNil(t, s.Root.Resolved)
		require.Equal(t, KindString, s.Root.Resolved.Types)
	})

	t.Run("enum value", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"$defs": {"A": {"type": "string"}}, "enum": [{"$ref": "#/$defs/A"}]}`), "")
		require.NoError(t, err)
		require.Len(t, s.Root.Enum, 1)
		require.Equal(t, map[string]any{"$ref": "#/$defs/A"}, s.Root.Enum[0].Decoded)
		require.JSONEq(t, `{"$ref": "#/$defs/A"}`, string(s.Root.Enum[0].Raw))
	})

	t.Run("const value", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"$defs": {"A": {"type": "string"}}, "const": {"$ref": "#/$defs/A"}}`), "")
		require.NoError(t, err)
		require.NotNil(t, s.Root.Const)
		require.Equal(t, map[string]any{"$ref": "#/$defs/A"}, s.Root.Const.Decoded)
	})

	t.Run("extension value", func(t *testing.T) {
		s, err := Load(ctx, []byte(`{"x-vendor": {"$ref": "#/$defs/A"}}`), "")
		require.NoError(t, err)
		require.Equal(t, map[string]any{"$ref": "#/$defs/A"}, s.Root.Extensions["x-vendor"])
	})
}
