package planner_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

const constTaggedPetDefs = `"$defs": {
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

const enumTaggedPetDefs = `"$defs": {
	"Cat": {
		"type": "object",
		"properties": {"petType": {"enum": ["cat", "kitten"]}, "name": {"type": "string"}},
		"required": ["petType", "name"]
	},
	"Dog": {
		"type": "object",
		"properties": {"petType": {"enum": ["dog", "puppy"]}, "bark": {"type": "boolean"}},
		"required": ["petType", "bark"]
	}
}`

const requiredButUnconstrainedPetDefs = `"$defs": {
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

const optionalTagPetDefs = `"$defs": {
	"Cat": {"type": "object", "properties": {"petType": {"const": "cat"}, "name": {"type": "string"}}},
	"Dog": {"type": "object", "properties": {"petType": {"const": "dog"}, "bark": {"type": "boolean"}}}
}`

const kindTaggedPetDefs = `"$defs": {
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

const doublyTaggedPetDefs = `"$defs": {
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

// branchID names the branch a case selects by what its plan represents: the `$ref` target
// for a factored branch, the field set for an inline one. Asserting on it is what tells a
// correct mapping from a swapped one.
func branchID(t *testing.T, p plan.CompilationPlan) string {
	t.Helper()

	switch r := p.Representation.(type) {
	case *plan.ReferenceRepresentation:
		return r.Name
	case *plan.ObjectRepresentation:
		names := make([]string, 0, len(r.Fields))
		for _, f := range r.Fields {
			names = append(names, f.Name)
		}
		slices.Sort(names)
		return strings.Join(names, "+")
	default:
		t.Fatalf("branch representation does not identify a branch: %#v", p.Representation)
		return ""
	}
}

func caseBranches(t *testing.T, cases []plan.LiteralCase) map[string]string {
	t.Helper()

	out := make(map[string]string, len(cases))
	for _, c := range cases {
		out[fmt.Sprint(c.Value)] = branchID(t, c.Plan)
	}
	return out
}

func hasKind(diags []plan.Diagnostic, k plan.DiagnosticKind) bool {
	for _, d := range diags {
		if d.Kind == k {
			return true
		}
	}
	return false
}

func hasSeverity(diags []plan.Diagnostic, s plan.Severity) bool {
	for _, d := range diags {
		if d.Severity == s {
			return true
		}
	}
	return false
}

func hasWarning(diags []plan.Diagnostic) bool {
	return hasSeverity(diags, plan.SeverityWarning)
}

func TestBuild_DeclaredDiscriminator(t *testing.T) {
	for _, tt := range []struct {
		name     string
		doc      string
		property string
		tag      plan.TagSource
		values   []any
		branches map[string]string
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
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "mapping is not symmetric",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Dog", "dog": "#/$defs/Cat"}
				},
				` + requiredButUnconstrainedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagAsserted,
			values:   []any{"dog", "cat"},
			branches: map[string]string{"dog": "/$defs/Cat", "cat": "/$defs/Dog"},
		},
		{
			name: "mapping by component name",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "Cat", "dog": "Dog"}},
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "mapping aliases every branch over an enum tag",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {
						"cat": "#/$defs/Cat", "kitten": "#/$defs/Cat",
						"dog": "#/$defs/Dog", "puppy": "#/$defs/Dog"
					}
				},
				` + enumTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog", "puppy"},
			branches: map[string]string{
				"cat": "/$defs/Cat", "kitten": "/$defs/Cat",
				"dog": "/$defs/Dog", "puppy": "/$defs/Dog",
			},
		},
		{
			name: "mapping alias beyond the branch const",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "kitten": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "kitten": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "partial mapping falls back to branch consts",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}, {"$ref": "#/$defs/Fish"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "#/$defs/Cat"}},
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog", "fish"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog", "fish": "/$defs/Fish"},
		},
		{
			name: "implicit mapping dispatches on the branch const",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "implicit mapping dispatches on every enum value",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				` + enumTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog", "puppy"},
			branches: map[string]string{
				"cat": "/$defs/Cat", "kitten": "/$defs/Cat",
				"dog": "/$defs/Dog", "puppy": "/$defs/Dog",
			},
		},
		{
			name: "inline const branches without mapping",
			doc: `{
				"oneOf": [
					{
						"type": "object",
						"properties": {"petType": {"const": "cat"}, "name": {"type": "string"}},
						"required": ["petType", "name"]
					},
					{
						"type": "object",
						"properties": {"petType": {"const": "dog"}, "bark": {"type": "boolean"}},
						"required": ["petType", "bark"]
					}
				],
				"discriminator": {"propertyName": "petType"}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "name+petType", "dog": "bark+petType"},
		},
		{
			name: "const in an allOf member",
			doc: `{
				"oneOf": [
					{"allOf": [
						{
							"type": "object",
							"properties": {"petType": {"type": "string"}, "name": {"type": "string"}},
							"required": ["name"]
						},
						{"type": "object", "properties": {"petType": {"const": "cat"}}, "required": ["petType"]}
					]},
					{"allOf": [
						{
							"type": "object",
							"properties": {"petType": {"type": "string"}, "bark": {"type": "boolean"}},
							"required": ["bark"]
						},
						{"type": "object", "properties": {"petType": {"const": "dog"}}, "required": ["petType"]}
					]}
				],
				"discriminator": {"propertyName": "petType"}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "name+petType", "dog": "bark+petType"},
		},
		{
			name: "declared wins over inferable property",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType", "mapping": {"cat": "Cat", "dog": "Dog"}},
				` + doublyTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "unconstrained tag is asserted, not proven",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + requiredButUnconstrainedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagAsserted,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "mapping omitting a value the branch accepts is asserted",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				},
				` + enumTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagAsserted,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "dangling mapping pointer",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Nope"}
				},
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagInferred,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
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
				` + constTaggedPetDefs + `
			}`,
			property: "petType",
			tag:      plan.TagInferred,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
			warn:     true,
		},
		{
			name: "propertyName absent from branches",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "nope"},
				` + requiredButUnconstrainedPetDefs + `
			}`,
			warn: true,
		},
		{
			name: "propertyName absent falls back to inference",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "nope"},
				` + kindTaggedPetDefs + `
			}`,
			property: "kind",
			tag:      plan.TagInferred,
			values:   []any{"circle", "square"},
			branches: map[string]string{"circle": "/$defs/Cat", "square": "/$defs/Dog"},
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
			name: "no mapping and no const leaves nothing to switch on",
			doc: `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType"},
				` + requiredButUnconstrainedPetDefs + `
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

			disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
			if tt.property == "" {
				require.False(t, ok, "expected no PropertyDispatch, got %#v", got.Plan.Dispatch)
				return
			}
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, tt.property, disp.Property)
			require.Equal(t, tt.tag, disp.Tag)
			require.Equal(t, tt.values, caseValues(disp.Cases))
			require.Equal(t, tt.branches, caseBranches(t, disp.Cases))

			require.Equal(t, tt.tag == plan.TagAsserted, hasSeverity(got.Diagnostics, plan.SeverityInfo),
				"an asserted dispatch, and only an asserted one, reports itself: %v", got.Diagnostics)
		})
	}
}

// TestBuild_AssertedDiscriminatorIsStaticButInexact pins the middle tier: OAS 3.0.3 line
// 2717 lets a discriminator "act as a 'hint' to shortcut validation and selection", so the
// dispatch is emitted, but nothing proved the branches disjoint and the plan says so
// (design §24).
func TestBuild_AssertedDiscriminatorIsStaticButInexact(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		`+requiredButUnconstrainedPetDefs+`
	}`)

	disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagAsserted, disp.Tag)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	require.True(t, hasKind(got.Diagnostics, plan.DiagnosticAssumed))
	require.True(t, hasSeverity(got.Diagnostics, plan.SeverityInfo))
	require.False(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
}

// TestBuild_ProvenDiscriminatorIsStatic is the top tier: a required const tag proves the
// branches disjoint, so nothing is assumed and no diagnostic is owed.
func TestBuild_ProvenDiscriminatorIsStatic(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		`+constTaggedPetDefs+`
	}`)

	disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagDeclared, disp.Tag)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	require.Empty(t, got.Diagnostics)
}

// TestBuild_UnrequiredDiscriminatorFallsBackToPredicateCount is the bottom tier: OAS 3.0.3
// line 2354 makes the discriminator property mandatory, so a union that leaves it optional
// cannot be dispatched at all and must be resolved by match counting (design §20.6, §22).
func TestBuild_UnrequiredDiscriminatorFallsBackToPredicateCount(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {
			"propertyName": "petType",
			"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
		},
		`+optionalTagPetDefs+`
	}`)

	disp, ok := got.Plan.Dispatch.(*plan.PredicateCountDispatch)
	require.True(t, ok, "expected PredicateCountDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, 1, disp.Minimum)
	require.Equal(t, 1, disp.Maximum)
	require.Equal(t, plan.RawEvaluation, got.Plan.Capability)
	require.True(t, hasWarning(got.Diagnostics))
}

// TestBuild_NestedDeclaredDiscriminators checks that a union nested inside a branch of
// another discriminated union keeps its own declaration (design §17).
func TestBuild_NestedDeclaredDiscriminators(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [
			{
				"type": "object",
				"properties": {"kind": {"const": "pet"}},
				"required": ["kind"],
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				}
			},
			{
				"type": "object",
				"properties": {"kind": {"const": "vehicle"}, "wheels": {"type": "integer"}},
				"required": ["kind", "wheels"]
			}
		],
		"discriminator": {"propertyName": "kind"},
		`+constTaggedPetDefs+`
	}`)

	outer, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "kind", outer.Property)
	require.Equal(t, plan.TagDeclared, outer.Tag)
	require.Equal(t, []any{"pet", "vehicle"}, caseValues(outer.Cases))

	inner, ok := outer.Cases[0].Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected a nested PropertyDispatch, got %T", outer.Cases[0].Plan.Dispatch)
	require.Equal(t, "petType", inner.Property)
	require.Equal(t, plan.TagDeclared, inner.Tag)
	require.Equal(t, []any{"cat", "dog"}, caseValues(inner.Cases))
	require.Equal(t, map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"}, caseBranches(t, inner.Cases))

	for _, d := range got.Diagnostics {
		require.NotContains(t, d.Message, "discriminator", "both declarations are usable")
	}
}

// TestBuild_NestedAssertedDiscriminatorIsAssumed checks the rollup: an
// asserted dispatch anywhere in the plan makes the whole plan an over-approximation.
func TestBuild_NestedAssertedDiscriminatorIsAssumed(t *testing.T) {
	got := buildDoc(t, `{
		"oneOf": [
			{
				"type": "object",
				"properties": {"kind": {"const": "pet"}},
				"required": ["kind"],
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {
					"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
				}
			},
			{
				"type": "object",
				"properties": {"kind": {"const": "vehicle"}, "wheels": {"type": "integer"}},
				"required": ["kind", "wheels"]
			}
		],
		"discriminator": {"propertyName": "kind"},
		`+requiredButUnconstrainedPetDefs+`
	}`)

	outer, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagDeclared, outer.Tag)

	inner, ok := outer.Cases[0].Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected a nested PropertyDispatch, got %T", outer.Cases[0].Plan.Dispatch)
	require.Equal(t, plan.TagAsserted, inner.Tag)
	require.True(t, hasKind(got.Diagnostics, plan.DiagnosticAssumed))
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

	require.IsType(t, &plan.NeverRepresentation{}, got.Plan.Representation)
	require.IsType(t, &plan.NoDispatch{}, got.Plan.Dispatch)
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
				` + constTaggedPetDefs + `
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
				` + constTaggedPetDefs + `
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
				` + constTaggedPetDefs + `
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
				` + constTaggedPetDefs + `
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
				` + constTaggedPetDefs + `
			}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDoc(t, tt.doc)

			disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, "petType", disp.Property)
			require.Equal(t, plan.TagDeclared, disp.Tag)
			require.Equal(t, []any{"cat", "dog"}, caseValues(disp.Cases))
			require.Equal(t, map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
				caseBranches(t, disp.Cases))
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

	disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, plan.TagInferred, disp.Tag)
}
