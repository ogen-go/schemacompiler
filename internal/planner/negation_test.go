package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

const subsetPetDefs = `"$defs": {
	"Cat": {"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}}},
	"Kitten": {"type": "object", "required": ["petType"], "properties": {"petType": {"const": "kitten"}}},
	"Dog": {"type": "object", "required": ["petType"], "properties": {"petType": {"const": "dog"}}}
}`

func buildNormalized(t *testing.T, doc string) planner.Result {
	t.Helper()

	s, err := frontend.Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)
	return planner.Build(norm.Normalize(ir.Compile(s.Root), 64), s.Registry)
}

// countNegations reports how many [plan.NegationPredicate]s p and its dispatch branches
// carry.
func countNegations(t *testing.T, p plan.CompilationPlan) int {
	t.Helper()

	n := 0
	for _, gp := range p.Validation.Predicates {
		if _, ok := gp.Expression.(plan.NegationPredicate); ok {
			n++
		}
	}
	planwalk.Dispatch(p.Dispatch, func(sub plan.CompilationPlan) { n += countNegations(t, sub) })
	return n
}

// TestBuild_SubsumedOneOfBranchKeepsResidualNegation pins issue #71. `oneOf` is
// ExactlyOne, so a branch whose accepted set is a *subset* of another branch's is
// unreachable and the wider branch loses the overlap: normalization rewrites
// ExactlyOne(A, B) with B ⊆ A into All(A, Not(B)) (design §15.2). That negation is the
// whole difference between the schema and a plain union, so the plan must carry it as a
// residual [plan.NegationPredicate] rather than drop it into a static dispatch that
// accepts the subsumed instances (design §24, docs/implementation.md invariant 4).
//
// The subset relation is what distinguishes this from the partially-overlapping shape the
// union-overlap check already rejects, so both orders and the declared/undeclared
// discriminator are covered: none of them may reach StaticDispatch.
func TestBuild_SubsumedOneOfBranchKeepsResidualNegation(t *testing.T) {
	const catOrKitten = `{"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}]}`
	const kitten = `{"$ref": "#/$defs/Kitten"}`

	for _, tt := range []struct {
		name string
		doc  string
	}{
		{
			name: "the second branch is subsumed by the first",
			doc: `{"oneOf": [` + catOrKitten + `, ` + kitten + `],
				"discriminator": {"propertyName": "petType"}, ` + subsetPetDefs + `}`,
		},
		{
			name: "the first branch is subsumed by the second",
			doc: `{"oneOf": [` + kitten + `, ` + catOrKitten + `],
				"discriminator": {"propertyName": "petType"}, ` + subsetPetDefs + `}`,
		},
		{
			name: "no declared discriminator",
			doc:  `{"oneOf": [` + catOrKitten + `, ` + kitten + `], ` + subsetPetDefs + `}`,
		},
		{
			name: "the subsuming branch is itself an allOf",
			doc: `{"oneOf": [{"allOf": [` + catOrKitten + `]}, ` + kitten + `],
				"discriminator": {"propertyName": "petType"}, ` + subsetPetDefs + `}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNormalized(t, tt.doc)

			require.Equal(t, plan.PredicateDispatch, got.Plan.Capability)
			require.Equal(t, plan.SoundOverApproximation, got.Exactness)
			require.NotZero(t, countNegations(t, got.Plan),
				"the residual negation must survive into the plan")
			require.True(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
		})
	}
}

// TestBuild_SubsumedOneOfBranchAmongThree covers the same subset relation with more than
// two branches. Normalization only proves subsumption pairwise (internal/norm/flatten.go),
// so the group stays an ExactlyOne and the union-overlap fallback catches it instead.
func TestBuild_SubsumedOneOfBranchAmongThree(t *testing.T) {
	got := buildNormalized(t, `{
		"oneOf": [
			{"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}]},
			{"$ref": "#/$defs/Dog"},
			{"$ref": "#/$defs/Kitten"}
		],
		"discriminator": {"propertyName": "petType"},
		`+subsetPetDefs+`}`)

	require.Equal(t, plan.PredicateDispatch, got.Plan.Capability)
	require.Equal(t, plan.SoundOverApproximation, got.Exactness)
	require.IsType(t, plan.PredicateCountDispatch{}, got.Plan.Dispatch)
}

// TestBuild_DisjointNestedCombinatorBranchesStayStatic pins the shapes #67 legitimately
// proves: when the nested alternatives do not reach into another branch there is no
// subsumption to rewrite, so nothing negated survives and the union stays a static
// property dispatch.
func TestBuild_DisjointNestedCombinatorBranchesStayStatic(t *testing.T) {
	const dog = `{"$ref": "#/$defs/Dog"}`

	for _, tt := range []struct {
		name   string
		branch string
	}{
		{name: "anyOf", branch: `{"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}]}`},
		{name: "allOf of an anyOf", branch: `{"allOf": [{"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}]}]}`},
		{name: "anyOf of an anyOf", branch: `{"anyOf": [{"anyOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}]}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNormalized(t, `{"oneOf": [`+tt.branch+`, `+dog+`],
				"discriminator": {"propertyName": "petType"}, `+subsetPetDefs+`}`)

			require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
			require.Zero(t, countNegations(t, got.Plan))
			disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, plan.TagDeclared, disp.Tag)
			require.Equal(t, []any{"cat", "kitten", "dog"}, caseValues(disp.Cases))
		})
	}
}
