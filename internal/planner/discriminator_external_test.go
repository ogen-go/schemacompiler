package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

func buildDoc(t *testing.T, doc string) planner.Result {
	t.Helper()

	s, err := frontend.Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)
	return planner.Build(ir.Compile(s.Root), s.Registry)
}

func caseValues(cases []plan.LiteralCase) []any {
	out := make([]any, len(cases))
	for i, c := range cases {
		out[i] = c.Value
	}
	return out
}

func TestBuild_DeclaredDiscriminator_Mapping(t *testing.T) {
	// Branches carry no const tag at all: only the declared mapping discriminates them.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		"$defs": {
			"Cat": {"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]},
			"Dog": {"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]}
		}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "petType", disp.Property)
	require.Equal(t, plan.TagDeclared, disp.Tag)
	require.Equal(t, []any{"cat", "dog"}, caseValues(disp.Cases))
	require.Empty(t, got.Diagnostics)
}

func TestBuild_DeclaredDiscriminator_MappingAliases(t *testing.T) {
	// Two values may select one schema; each becomes its own case.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "kitten": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		"$defs": {
			"Cat": {"type": "object", "properties": {"petType": {"type": "string"}}},
			"Dog": {"type": "object", "properties": {"petType": {"type": "string"}}}
		}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, []any{"cat", "kitten", "dog"}, caseValues(disp.Cases))
}

func TestBuild_DeclaredDiscriminator_ImplicitMapping(t *testing.T) {
	// Without `mapping`, a branch is selected by the component name of its $ref.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "petType"},
		"$defs": {
			"Cat": {"type": "object", "properties": {"petType": {"type": "string"}}},
			"Dog": {"type": "object", "properties": {"petType": {"type": "string"}}}
		}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagDeclared, disp.Tag)
	require.Equal(t, []any{"Cat", "Dog"}, caseValues(disp.Cases))
}

func TestBuild_DeclaredDiscriminator_WinsOverInference(t *testing.T) {
	// Both a declared property and a structurally inferable one are present; the
	// declared one decides.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "petType", "mapping": {"cat": "Cat", "dog": "Dog"}},
		"$defs": {
			"Cat": {"type": "object", "properties": {"kind": {"const": "circle"}}, "required": ["kind"]},
			"Dog": {"type": "object", "properties": {"kind": {"const": "square"}}, "required": ["kind"]}
		}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "petType", disp.Property)
	require.Equal(t, plan.TagDeclared, disp.Tag)
	require.Equal(t, []any{"cat", "dog"}, caseValues(disp.Cases))
}

func TestBuild_DeclaredDiscriminator_UnusableFallsBack(t *testing.T) {
	// Inline branches with neither a mapping nor a const on the declared property carry
	// no declared value: the plan falls back to structural inference with a warning.
	got := buildDoc(t, `{
		"oneOf": [
			{"type": "object", "properties": {"kind": {"const": "circle"}}, "required": ["kind"]},
			{"type": "object", "properties": {"kind": {"const": "square"}}, "required": ["kind"]}
		],
		"discriminator": {"propertyName": "petType"}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "kind", disp.Property)
	require.Equal(t, plan.TagInferred, disp.Tag)
	require.NotEmpty(t, got.Diagnostics)
	require.Equal(t, plan.SeverityWarning, got.Diagnostics[0].Severity)
}

func TestBuild_DeclaredDiscriminator_BranchConstOverridesImplicitName(t *testing.T) {
	// No mapping, but each branch constrains the declared property: those consts, not
	// the component names, are the case values.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "petType"},
		"$defs": {
			"Cat": {"type": "object", "properties": {"petType": {"const": "cat"}}, "required": ["petType"]},
			"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
		}
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, []any{"cat", "dog"}, caseValues(disp.Cases))
}

func TestBuild_DeclaredDiscriminator_DuplicateValueFallsBack(t *testing.T) {
	// Two branches mapped from the same value cannot be told apart.
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "petType", "mapping": {"pet": "#/$defs/Cat"}},
		"$defs": {
			"Cat": {"type": "object", "properties": {"petType": {"const": "pet"}}, "required": ["petType"]},
			"Dog": {"type": "object", "properties": {"petType": {"const": "pet"}}, "required": ["petType"]}
		}
	}`)

	_, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.False(t, ok, "duplicate discriminator values must not yield PropertyDispatch")
	require.NotEmpty(t, got.Diagnostics)
}

func TestBuild_InferredDiscriminatorTagsAsInferred(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [
			{"type": "object", "properties": {"kind": {"const": "circle"}}, "required": ["kind"]},
			{"type": "object", "properties": {"kind": {"const": "square"}}, "required": ["kind"]}
		]
	}`)

	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagInferred, disp.Tag)
}
