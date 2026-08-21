package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// petDefs declares three tagged object schemas. Each one requires petType and pins it to
// its own const, which is what makes the branches pairwise disjoint and the union
// statically dispatchable (design §18, §15.3).
const petDefs = `"$defs": {
	"Cat": {
		"type": "object",
		"properties": {"petType": {"const": "cat"}, "name": {"type": "string"}},
		"required": ["petType", "name"]
	},
	"Dog": {
		"type": "object",
		"properties": {"petType": {"const": "dog"}, "bark": {"type": "boolean"}},
		"required": ["petType", "bark"]
	},
	"Fish": {
		"type": "object",
		"properties": {"petType": {"const": "fish"}, "fins": {"type": "integer"}},
		"required": ["petType", "fins"]
	}
}`

// untaggedPetDefs is petDefs without the const tags: the branches declare petType but
// nothing observable in the instance tells them apart, so no static dispatch is sound.
const untaggedPetDefs = `"$defs": {
	"Cat": {
		"type": "object",
		"properties": {"petType": {"type": "string"}, "name": {"type": "string"}},
		"required": ["petType", "name"]
	},
	"Dog": {
		"type": "object",
		"properties": {"petType": {"type": "string"}, "bark": {"type": "boolean"}},
		"required": ["petType", "bark"]
	}
}`

// optionalTagPetDefs pins petType to a const but does not require it, so an instance
// omitting it satisfies both branches.
const optionalTagPetDefs = `"$defs": {
	"Cat": {"type": "object", "properties": {"petType": {"const": "cat"}, "name": {"type": "string"}}},
	"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}, "bark": {"type": "boolean"}}}
}`

// kindDefs carries a structurally inferable const on kind only: petType is declared but
// unconstrained, so a discriminator naming it cannot drive dispatch.
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

// taggedKindDefs carries two usable tags, kind first, so a declared discriminator naming
// petType and structural inference disagree about which property to switch on.
const taggedKindDefs = `"$defs": {
	"Cat": {
		"type": "object",
		"properties": {"kind": {"const": "circle"}, "petType": {"const": "cat"}},
		"required": ["kind", "petType"]
	},
	"Dog": {
		"type": "object",
		"properties": {"kind": {"const": "square"}, "petType": {"const": "dog"}},
		"required": ["kind", "petType"]
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
				"$defs": {
					"Cat": {
						"type": "object",
						"properties": {"petType": {"enum": ["cat", "kitten"]}},
						"required": ["petType"]
					},
					"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
				}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog"},
		},
		{
			name: "partial mapping falls back to branch consts",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}, {"$ref": "#/$defs/Fish"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "#/$defs/Cat"}},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog", "fish"},
		},
		{
			name: "implicit mapping dispatches on the branch const",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				` + petDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
		},
		{
			name: "enum tag without mapping",
			doc: `{
				"oneOf": [
					{
						"type": "object",
						"properties": {"petType": {"enum": ["cat", "kitten"]}},
						"required": ["petType"]
					},
					{"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
				],
				"discriminator": {"propertyName": "petType"}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog"},
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
				` + taggedKindDefs + `
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
			property: "petType",
			tag:      plan.TagInferred,
			values:   []any{"cat", "dog"},
			warn:     true,
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
			property: "petType",
			tag:      plan.TagInferred,
			values:   []any{"cat", "dog"},
			warn:     true,
		},
		{
			name: "mapping omits a value the branch accepts",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "#/$defs/Cat"}},
				"$defs": {
					"Cat": {
						"type": "object",
						"properties": {"petType": {"enum": ["cat", "kitten"]}},
						"required": ["petType"]
					},
					"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
				}
			}`,
			warn: true,
		},
		{
			name: "propertyName absent from branches",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "nope"},
				` + untaggedPetDefs + `
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
		{
			name: "tag is declared but unconstrained",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + untaggedPetDefs + `
			}`,
			warn: true,
		},
		{
			name: "tag is constrained but not required",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + optionalTagPetDefs + `
			}`,
			warn: true,
		},
		{
			name: "identical branches",
			doc: `{
				"oneOf": [
					{"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]},
					{"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]}
				],
				"discriminator": {"propertyName": "petType"}
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

// TestBuild_UnprovenDiscriminatorFallsBackToPredicateCount pins the soundness contract of
// the fallback: a declaration that is not backed by a required const/enum tag must not
// produce a static dispatch, and the resulting plan must report the runtime cost
// (design §20.6, §22) and the loss of exactness (design §24).
func TestBuild_UnprovenDiscriminatorFallsBackToPredicateCount(t *testing.T) {
	const mapping = `"discriminator": {
		"propertyName": "petType",
		"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
	}`

	for _, tt := range []struct {
		name string
		doc  string
	}{
		{
			name: "unconstrained tag",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				` + mapping + `,
				` + untaggedPetDefs + `
			}`,
		},
		{
			name: "optional tag",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				` + mapping + `,
				` + optionalTagPetDefs + `
			}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDoc(t, tt.doc)

			disp, ok := got.Plan.Dispatch.(plan.PredicateCountDispatch)
			require.True(t, ok, "expected PredicateCountDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, 1, disp.Minimum)
			require.Equal(t, 1, disp.Maximum)
			require.Equal(t, plan.PredicateDispatch, got.Plan.Capability)
			require.Equal(t, plan.SoundOverApproximation, got.Exactness)
			require.True(t, hasWarning(got.Diagnostics))
		})
	}
}

// TestBuild_ProvenDiscriminatorIsStatic is the control: with a required const tag the
// proof holds, so the plan is a static dispatch and may claim exactness.
func TestBuild_ProvenDiscriminatorIsStatic(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		`+petDefs+`
	}`)

	_, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	require.Equal(t, plan.ExactWithValidation, got.Exactness)
	require.False(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
}

// TestBuild_IdenticalBranchesAreUninhabited pins design §15.1: ExactlyOne(A, A) is Never,
// so a union of interchangeable branches is uninhabited however it is discriminated.
func TestBuild_IdenticalBranchesAreUninhabited(t *testing.T) {
	s, err := frontend.Load(context.Background(), []byte(`{
		"oneOf": [
			{"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]},
			{"type": "object", "properties": {"petType": {"type": "string"}}, "required": ["petType"]}
		],
		"discriminator": {"propertyName": "petType"}
	}`), "")
	require.NoError(t, err)

	got := planner.Build(norm.Normalize(ir.Compile(s.Root), 64), s.Registry)

	require.IsType(t, plan.NeverRepresentation{}, got.Plan.Representation)
	require.IsType(t, plan.NoDispatch{}, got.Plan.Dispatch)
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
