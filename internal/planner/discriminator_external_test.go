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

// petDefs declares three interchangeable tagged object schemas: every branch carries the
// petType property, so only the discriminator tells them apart.
const petDefs = `"$defs": {
	"Cat": {"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]},
	"Dog": {"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]},
	"Fish": {"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]}
}`

// kindDefs adds a structurally inferable const tag on a second property, so a declared
// discriminator and structural inference disagree about which property to switch on.
const kindDefs = `"$defs": {
	"Cat": {
		"type": "object",
		"properties": {"petType": {"type": "string"}, "kind": {"const": "circle"}},
		"required": ["petType", "kind"]
	},
	"Dog": {
		"type": "object",
		"properties": {"petType": {"type": "string"}, "kind": {"const": "square"}},
		"required": ["petType", "kind"]
	}
}`

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

func hasWarning(diags []plan.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == plan.SeverityWarning {
			return true
		}
	}
	return false
}

func TestBuild_DeclaredDiscriminator(t *testing.T) {
	for _, tt := range []struct {
		name     string
		doc      string
		property string
		tag      plan.TagSource
		values   []any
		warn     bool
	}{
		{
			name: "mapping",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "mapping by component name",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "Cat", "dog": "Dog"}},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "mapping aliases one branch",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "kitten": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog"},
		},
		{
			name: "partial mapping falls back to component names",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}, {"$ref": "#/$defs/Fish"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "#/$defs/Cat"}},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "Dog", "Fish"},
		},
		{
			name: "implicit mapping",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"Cat", "Dog"},
		},
		{
			name: "branch const beats implicit mapping",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				"$defs": {
					"Cat": {"type": "object", "properties": {"petType": {"const": "cat"}}, "required": ["petType"]},
					"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
				}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "inline const branches without mapping",
			doc: `{
				"oneOf": [
					{"type": "object", "properties": {"petType": {"const": "cat"}}, "required": ["petType"]},
					{"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
				],
				"discriminator": {"propertyName": "petType"}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "declared wins over inferable property",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "Cat", "dog": "Dog"}},
				` + kindDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "dangling mapping pointer",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Nope"}
				},
				` + petDefs + `
			}`,
			warn: true,
		},
		{
			name: "mapping targets a schema outside the union",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "fish": "#/$defs/Fish"}
				},
				` + petDefs + `
			}`,
			warn: true,
		},
		{
			name: "propertyName absent from branches",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "nope"},
				` + petDefs + `
			}`,
			warn: true,
		},
		{
			name: "propertyName absent falls back to inference",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "nope"},
				` + kindDefs + `
			}`,
			property: "kind",
			tag:      plan.TagInferred,
			values:   []any{"circle", "square"},
			warn:     true,
		},
		{
			name: "one value selects two branches",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"pet": "#/$defs/Cat"}},
				"$defs": {
					"Cat": {"type": "object", "properties": {"petType": {"const": "pet"}}, "required": ["petType"]},
					"Dog": {"type": "object", "properties": {"petType": {"const": "pet"}}, "required": ["petType"]}
				}
			}`,
			warn: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDoc(t, tt.doc)

			require.Equal(t, tt.warn, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)

			disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
			if tt.property == "" {
				require.False(t, ok, "expected no PropertyDispatch, got %#v", got.Plan.Dispatch)
				return
			}
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, tt.property, disp.Property)
			require.Equal(t, tt.tag, disp.Tag)
			require.Equal(t, tt.values, caseValues(disp.Cases))
		})
	}
}

// TestBuild_DeclaredDiscriminatorCombinatorShapes locks down the union shapes normalization
// rewrites: the declaration must survive flattening, distribution and the oneOf → anyOf
// rewrite, whichever combinator it was declared on.
func TestBuild_DeclaredDiscriminatorCombinatorShapes(t *testing.T) {
	const mapping = `"discriminator": {
		"propertyName": "petType",
		"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
	}`

	for _, tt := range []struct {
		name string
		doc  string
	}{
		{
			name: "anyOf",
			doc: `{
				"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				` + mapping + `,
				` + petDefs + `
			}`,
		},
		{
			name: "oneOf branches from allOf",
			doc: `{
				"oneOf": [
					{"allOf": [{"$ref": "#/$defs/Cat"}]},
					{"allOf": [{"$ref": "#/$defs/Dog"}]}
				],
				` + mapping + `,
				` + petDefs + `
			}`,
		},
		{
			name: "anyOf branches from allOf",
			doc: `{
				"anyOf": [
					{"allOf": [{"$ref": "#/$defs/Cat"}]},
					{"allOf": [{"$ref": "#/$defs/Dog"}]}
				],
				` + mapping + `,
				` + petDefs + `
			}`,
		},
		{
			name: "allOf wrapping the union",
			doc: `{
				"allOf": [
					{"type": "object"},
					{
						"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
						` + mapping + `
					}
				],
				` + petDefs + `
			}`,
		},
		{
			name: "oneOf nested in anyOf",
			doc: `{
				"anyOf": [
					{
						"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
						` + mapping + `
					}
				],
				` + petDefs + `
			}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDoc(t, tt.doc)

			disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, "petType", disp.Property)
			require.Equal(t, plan.TagDeclared, disp.Tag)
			require.Equal(t, []any{"cat", "dog"}, caseValues(disp.Cases))
			require.False(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
		})
	}
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
