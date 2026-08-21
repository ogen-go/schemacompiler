package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func compile(t *testing.T, schema string) plan.CompilationPlan {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res.Plan
}

func objectFields(t *testing.T, p plan.CompilationPlan) map[string]plan.FieldRepresentation {
	t.Helper()
	obj, ok := p.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "expected object representation, got %T", p.Representation)
	return obj.Fields
}

func TestCompile_NodeMetadata(t *testing.T) {
	p := compile(t, `{
		"type": "string",
		"title": "Name",
		"description": "A name.",
		"deprecated": true,
		"readOnly": true,
		"default": "anon",
		"examples": ["a", 1],
		"xml": {"name": "Name", "attribute": true},
		"x-ogen-name": "Nm"
	}`)
	m := p.Metadata
	require.Equal(t, "Name", m.Title)
	require.Equal(t, "A name.", m.Description)
	require.True(t, m.Deprecated)
	require.True(t, m.ReadOnly)
	require.False(t, m.WriteOnly)
	require.JSONEq(t, `"anon"`, string(m.Default))
	require.Equal(t, [][]byte{[]byte(`"a"`), []byte(`1`)}, m.Examples)
	require.Equal(t, &plan.XMLMetadata{Name: "Name", Attribute: true}, m.XML)
	require.Equal(t, map[string]any{"x-ogen-name": "Nm"}, m.Extensions)
}

func TestCompile_FieldMetadata(t *testing.T) {
	fields := objectFields(t, compile(t, `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"title": "Identifier",
				"description": "Opaque id.",
				"deprecated": true,
				"x-ogen-type": "uuid.UUID"
			},
			"plain": {"type": "string"}
		},
		"required": ["id"]
	}`))

	id := fields["id"]
	require.Equal(t, "Identifier", id.Metadata.Title)
	require.Equal(t, "Opaque id.", id.Metadata.Description)
	require.True(t, id.Metadata.Deprecated)
	require.Equal(t, map[string]any{"x-ogen-type": "uuid.UUID"}, id.Metadata.Extensions)
	require.Equal(t, plan.Metadata{}, fields["plain"].Metadata)
}

func TestCompile_NestedFieldMetadata(t *testing.T) {
	fields := objectFields(t, compile(t, `{
		"type": "object",
		"properties": {
			"nested": {
				"type": "object",
				"properties": {"inner": {"type": "string", "title": "Inner"}}
			}
		}
	}`))

	nested, ok := fields["nested"].Representation.(plan.ObjectRepresentation)
	require.True(t, ok)
	require.Equal(t, "Inner", nested.Fields["inner"].Metadata.Title)
}

func TestCompile_AllOfBranchFieldMetadata(t *testing.T) {
	fields := objectFields(t, compile(t, `{
		"allOf": [
			{"type": "object", "properties": {"a": {"type": "string", "title": "A"}}},
			{"type": "object", "properties": {"b": {"type": "string", "title": "B"}}}
		]
	}`))
	require.Equal(t, "A", fields["a"].Metadata.Title)
	require.Equal(t, "B", fields["b"].Metadata.Title)
}

func TestCompile_ArrayItemMetadata(t *testing.T) {
	p := compile(t, `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {"a": {"type": "string", "x-tag": "t"}}
		}
	}`)
	arr, ok := p.Representation.(plan.ArrayRepresentation)
	require.True(t, ok, "got %T", p.Representation)
	obj, ok := arr.Rest.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", arr.Rest)
	require.Equal(t, map[string]any{"x-tag": "t"}, obj.Fields["a"].Metadata.Extensions)
}

func TestCompile_DefinitionMetadata(t *testing.T) {
	p := compile(t, `{
		"$defs": {"Named": {"type": "string", "title": "Named", "x-ogen-name": "N"}},
		"$ref": "#/$defs/Named"
	}`)
	graph, ok := p.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok, "got %T", p.Resolution)
	def, ok := graph.Definitions["/$defs/Named"]
	require.True(t, ok)
	require.Equal(t, "Named", def.Metadata.Title)
	require.Equal(t, map[string]any{"x-ogen-name": "N"}, def.Metadata.Extensions)
}
