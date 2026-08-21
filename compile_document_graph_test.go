package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

const capabilityDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        b: {$ref: '#/components/schemas/B'}
    B:
      type: object
      unevaluatedProperties: false
    C:
      type: string
`

// Design §22: a plan's capability is at least the capability of what it references, so a
// generator gating per plan (docs/integration.md §6) never emits a type referring to one
// it must refuse.
func TestCompileDocumentCapabilityRollUp(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, capabilityDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	require.Equal(t, plan.EvaluationStateValidation, res.Plans["/components/schemas/B"].Capability)
	require.Equal(t, plan.EvaluationStateValidation, res.Plans["/components/schemas/A"].Capability,
		"A references B, so it costs at least as much")
	require.Equal(t, plan.DirectGoType, res.Plans["/components/schemas/C"].Capability,
		"an unrelated component must not be dragged up")
	require.Equal(t, plan.EvaluationStateValidation, res.Capability)
}

// Compile rolls the same way into every named definition it hands back.
func TestCompileDefinitionCapabilityRollUp(t *testing.T) {
	const schema = `{
		"$ref": "#/$defs/A",
		"$defs": {
			"A": {"type": "object", "properties": {"b": {"$ref": "#/$defs/B"}}},
			"B": {"type": "object", "unevaluatedProperties": false}
		}
	}`

	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	graph, ok := res.Plan.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok, "expected StaticReferenceGraph, got %T", res.Plan.Resolution)
	require.Equal(t, plan.EvaluationStateValidation, graph.Definitions["/$defs/A"].Capability)
	require.Equal(t, plan.EvaluationStateValidation, graph.Definitions["/$defs/B"].Capability)
}

// The document's Plans map is the reference graph, so component plans carry no graph of
// their own (docs/integration.md, "Whole-document compilation").
func TestCompileDocumentPlansAreFullyResolved(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, petstoreDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	for id, p := range res.Plans {
		require.IsType(t, plan.FullyResolved{}, p.Resolution, "plan %q", id)
	}
}
