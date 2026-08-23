package frontend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

func marshalNode(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	root, err := loadDocument([]byte(doc))
	require.NoError(t, err)
	return root
}

// roundTrip renders the tree in block style, so a node the pass rebuilt and one it left
// alone are compared by content rather than by the style they were written in.
func roundTrip(t *testing.T, n *yaml.Node) string {
	t.Helper()
	clearStyle(n)
	out, err := yaml.Marshal(n)
	require.NoError(t, err)
	return string(out)
}

func clearStyle(n *yaml.Node) {
	if n == nil {
		return
	}
	n.Style = 0
	for _, c := range n.Content {
		clearStyle(c)
	}
}

// TestExpandBooleanSchemas pins both halves: every keyword libopenapi cannot parse a
// boolean in gets the object spelling, and everything else is left exactly as written.
// The second half is the one that matters — `not` and `if` are ordinary property names,
// and a boolean under `const` or `enum` is instance data.
func TestExpandBooleanSchemas(t *testing.T) {
	for _, tt := range []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "true becomes the empty schema",
			doc:  `{"not": true}`,
			want: "not: {}\n",
		},
		{
			name: "false becomes a negated empty schema",
			doc:  `{"contains": false}`,
			want: "contains:\n    not: {}\n",
		},
		{
			name: "nested inside a schema list",
			doc:  `{"allOf": [{"if": true}]}`,
			want: "allOf:\n    - if: {}\n",
		},
		{
			name: "nested inside a schema map",
			doc:  `{"properties": {"a": {"propertyNames": false}}}`,
			want: "properties:\n    a:\n        propertyNames:\n            not: {}\n",
		},
		{
			name: "a property named not is instance data",
			doc:  `{"properties": {"not": true}}`,
			want: "properties:\n    not: true\n",
		},
		{
			name: "a definition named if is instance data",
			doc:  `{"$defs": {"if": true}}`,
			want: "$defs:\n    if: true\n",
		},
		{
			name: "const holds a boolean, not a schema",
			doc:  `{"const": {"not": true}}`,
			want: "const:\n    not: true\n",
		},
		{
			name: "enum holds booleans, not schemas",
			doc:  `{"enum": [true, false]}`,
			want: "enum:\n    - true\n    - false\n",
		},
		{
			name: "an unknown keyword carries no schema",
			doc:  `{"x-thing": {"not": true}}`,
			want: "x-thing:\n    not: true\n",
		},
		{
			name: "items is left to libopenapi",
			doc:  `{"items": false}`,
			want: "items: false\n",
		},
		{
			name: "additionalProperties is left to libopenapi",
			doc:  `{"additionalProperties": false}`,
			want: "additionalProperties: false\n",
		},
		{
			name: "a non-boolean schema is untouched",
			doc:  `{"not": {"type": "string"}}`,
			want: "not:\n    type: string\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := marshalNode(t, tt.doc)
			expandBooleanSchemas(root)
			require.Equal(t, tt.want, roundTrip(t, root))
		})
	}
}

// TestExpandBooleanSchemasKeepsPosition pins that a diagnostic about the rewritten schema
// still points at the boolean the author wrote, not at line zero.
func TestExpandBooleanSchemasKeepsPosition(t *testing.T) {
	root := marshalNode(t, "not:\n  false\n")
	expandBooleanSchemas(root)

	rewritten := root.Content[1]
	require.Equal(t, yaml.MappingNode, rewritten.Kind)
	require.Equal(t, 2, rewritten.Line)
	require.Equal(t, 3, rewritten.Column)
}

// TestBooleanSchemaKeywordsLoad pins the reason the pass exists: each of these fails
// libopenapi's build with "expected a single schema object" when spelled as a boolean.
func TestBooleanSchemaKeywordsLoad(t *testing.T) {
	for keyword := range booleanSchemaKeywords {
		for _, value := range []string{"true", "false"} {
			t.Run(keyword+"="+value, func(t *testing.T) {
				_, err := Load(context.Background(), []byte(`{"`+keyword+`": `+value+`}`), "")
				require.NoError(t, err)
			})
		}
	}
}
