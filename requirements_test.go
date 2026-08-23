package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func pointers(locs []plan.Location) []string {
	out := make([]string, 0, len(locs))
	for _, l := range locs {
		out = append(out, l.Pointer)
	}
	return out
}

// TestRequirementsSpanTheDocument pins that a document's requirements are the union over
// every schema in it, not the root's (design §25): a backend has to satisfy all of them,
// and the one that bites may well be inside a `$ref` target.
func TestRequirementsSpanTheDocument(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(), []byte(`{
		"type": "object",
		"properties": {"a": {"$ref": "#/$defs/tagged"}},
		"$defs": {
			"tagged": {"type": "array", "uniqueItems": true}
		}
	}`), schemacompiler.Options{})
	require.NoError(t, err)

	require.Equal(t, []string{"/$defs/tagged"}, pointers(res.Requirements.JSONEquality),
		"a requirement inside a $ref target must reach the document's Requirements")
}

// TestRequirementsCarryTheirLocation keeps a requirement findable: a consumer that cannot
// discharge one has to be able to say which schema it came from.
func TestRequirementsCarryTheirLocation(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(), []byte(`{
		"type": "object",
		"properties": {"code": {"type": "integer"}}
	}`), schemacompiler.Options{})
	require.NoError(t, err)

	require.Len(t, res.Requirements.UnboundedNumeric, 1)
	require.Contains(t, res.Requirements.UnboundedNumeric[0].Pointer, "code")
}

// TestRequirementsAreStable keeps the field reproducible across runs: definitions are
// compiled by ranging a map, so an unsorted union would reorder between runs and make a
// golden or a diff useless.
func TestRequirementsAreStable(t *testing.T) {
	const doc = `{
		"$defs": {
			"a": {"type": "array", "uniqueItems": true},
			"b": {"type": "array", "uniqueItems": true},
			"c": {"type": "array", "uniqueItems": true}
		},
		"allOf": [{"$ref": "#/$defs/a"}, {"$ref": "#/$defs/b"}, {"$ref": "#/$defs/c"}]
	}`

	first, err := schemacompiler.Compile(context.Background(), []byte(doc), schemacompiler.Options{})
	require.NoError(t, err)
	require.Len(t, first.Requirements.JSONEquality, 3)

	for range 8 {
		again, err := schemacompiler.Compile(context.Background(), []byte(doc), schemacompiler.Options{})
		require.NoError(t, err)
		require.Equal(t, pointers(first.Requirements.JSONEquality), pointers(again.Requirements.JSONEquality))
	}
}

// TestNoRequirementsForAPlainDocument keeps an empty Requirements meaningful.
func TestNoRequirementsForAPlainDocument(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(), []byte(`{
		"type": "object",
		"properties": {"name": {"type": "string", "minLength": 1}},
		"required": ["name"]
	}`), schemacompiler.Options{})
	require.NoError(t, err)
	require.True(t, res.Requirements.Empty(), "got %+v", res.Requirements)
}

// TestDocumentRequirementsSpanEveryComponent pins the CompileDocument path: a backend
// generating a whole OpenAPI document must discharge what every component asks for, not
// what the component it happens to be looking at asks for. The union is over the
// components and over the nested `$ref` targets they reach.
func TestDocumentRequirementsSpanEveryComponent(t *testing.T) {
	const doc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Tagged:
      type: array
      uniqueItems: true
    Counter:
      type: integer
    Named:
      type: object
      properties:
        name: {type: string, pattern: '^\w+$'}
    Plain:
      type: string
`
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, doc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	require.Equal(t, []string{"/components/schemas/Tagged"}, pointers(res.Requirements.JSONEquality))
	require.Equal(t, []string{"/components/schemas/Counter"}, pointers(res.Requirements.UnboundedNumeric))
	require.Len(t, res.Requirements.ECMARegex, 1)
	require.Contains(t, res.Requirements.ECMARegex[0].Pointer, "Named")
}

// TestDocumentRequirementsEmptyWhenNothingIsAsked keeps an empty Requirements meaningful
// on the document path too: a consumer must be able to tell "nothing to discharge" from
// "the field was never populated", which is what this path used to return.
func TestDocumentRequirementsEmptyWhenNothingIsAsked(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, petstoreDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)
	require.True(t, res.Requirements.Empty(), "got %+v", res.Requirements)
}
