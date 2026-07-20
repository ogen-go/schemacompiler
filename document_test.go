package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestCompileRefDefinitions(t *testing.T) {
	const schema = `{
		"type": "object",
		"properties": {"child": {"$ref": "#/$defs/Leaf"}},
		"$defs": {"Leaf": {"type": "string"}}
	}`

	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	// The document-level resolution graph must carry the referenced definition.
	graph, ok := res.Plan.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok, "expected StaticReferenceGraph, got %T", res.Plan.Resolution)
	require.Len(t, graph.Definitions, 1, "one $ref target should become a definition")

	for id, def := range graph.Definitions {
		require.NotEmpty(t, id, "definition must have a SchemaID key")
		prim, ok := def.Representation.(plan.PrimitiveRepresentation)
		require.True(t, ok, "Leaf should be a primitive, got %T", def.Representation)
		require.Equal(t, plan.KindString, prim.Kind)
		require.Equal(t, plan.DirectGoType, def.Capability)
	}

	// The referencing field must point at the definition by name, not inline it.
	obj, ok := res.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "root should be an object, got %T", res.Plan.Representation)
	child, ok := obj.Fields["child"]
	require.True(t, ok, "root should have a child field")
	ref, ok := child.Representation.(plan.ReferenceRepresentation)
	require.True(t, ok, "child should be a reference, got %T", child.Representation)
	_, inGraph := graph.Definitions[plan.SchemaID(ref.Name)]
	require.True(t, inGraph, "child ref %q must resolve to a definition", ref.Name)
}

func TestCompileGuardedRecursion(t *testing.T) {
	// A linked list: guarded recursion (every cycle descends into a property), so it is
	// representable and must not be flagged Unsupported.
	const schema = `{
		"$ref": "#/$defs/Node",
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {"next": {"$ref": "#/$defs/Node"}}
			}
		}
	}`

	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	graph, ok := res.Plan.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok, "expected StaticReferenceGraph, got %T", res.Plan.Resolution)
	require.NotEmpty(t, graph.Definitions, "recursive Node should be a definition")

	for _, d := range res.Diagnostics {
		require.NotEqual(t, plan.SeverityError, d.Severity,
			"guarded recursion must not produce an error diagnostic: %s", d.Message)
	}
	require.NotEqual(t, plan.Unsupported, res.Capability, "guarded recursion is supported")
}
