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

// TestBuild_SubsumedOneOfBranchIsNotClaimedExact pins issue #71. `oneOf` is ExactlyOne,
// so a branch whose accepted set is a *subset* of another branch's is unreachable and the
// wider branch loses the overlap: normalization rewrites ExactlyOne(A, B) with B ⊆ A into
// All(A, Not(B)) (design §15.2). Dropping that negation is what made the plan accept the
// subsumed instances while reporting ExactWithValidation.
//
// The negation is not emitted here, because its operand is a `$ref` and a reference is
// planned from its identity alone, so nothing here proves the target exact and negating an
// over-approximation rejects valid instances (#82). What the plan must not do is keep
// claiming exactness: it reports DeclaredIncomplete and says which constraint went
// unenforced. Once a reference's exactness consults its target the predicate is emitted, and
// these move to PredicateDispatch with a non-zero negation count.
//
// The subset relation is what distinguishes this from the partially-overlapping shape the
// union-overlap check already rejects, so both orders and the declared/undeclared
// discriminator are covered.
func TestBuild_SubsumedOneOfBranchIsNotClaimedExact(t *testing.T) {
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

			require.Equal(t, plan.DeclaredIncomplete, got.Exactness,
				"the plan accepts the subsumed instances and nothing in it rejects them")
			require.True(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
			require.Zero(t, countNegations(t, got.Plan),
				"blocked on a reference exactness that does not consult its target")
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
	require.Equal(t, plan.ExactWithValidation, got.Exactness)
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

// TestBuild_NegationIsGatedOnNestedExactness pins issue #82. `not S` accepts exactly what
// S rejects, so negation inverts the approximation polarity of its operand: a nested plan
// that over-approximates S makes the negation *under*-approximate `not S` and reject valid
// instances, which §24 never permits in either direction. The predicate is therefore
// emitted only when the nested plan is exactly modeled; otherwise the negation is dropped,
// which widens the outer plan (always sound) and is reported.
//
// The decision is the nested plan's own [plan.Exactness], one rung per row below, plus the
// one case exactness cannot see: a reference, whose target is planned by a separate
// [planner.BuildAt] call, so a reference reads as exact whatever its target turns out to be.
func TestBuild_NegationIsGatedOnNestedExactness(t *testing.T) {
	for _, tt := range []struct {
		name   string
		doc    string
		reason string
		emit   bool
	}{
		{
			name:   "nested ExactPureRepresentation",
			doc:    `{"not": {"type": "integer"}}`,
			reason: "the representation alone reproduces the schema, so negating it is exact too",
			emit:   true,
		},
		{
			name:   "nested ExactWithValidation",
			doc:    `{"not": {"type": "string", "minLength": 3}}`,
			reason: "the residual validator closes the representation's gap, so the accepted set is the schema's",
			emit:   true,
		},
		{
			name:   "nested ExactWithValidation through a match count",
			doc:    `{"not": {"type": "array", "contains": {"type": "string"}}}`,
			reason: "PredicateDispatch prices the match count, it does not approximate it (#100)",
			emit:   true,
		},
		{
			name:   "nested object is exact inside its fields",
			doc:    `{"not": {"type": "object", "properties": {"a": {"type": "string", "minLength": 1}}}}`,
			reason: "a field carries the whole sub-plan written there (#68)",
			emit:   true,
		},
		{
			name:   "nested array is exact inside its items",
			doc:    `{"not": {"type": "array", "items": {"type": "string", "minLength": 5}}}`,
			reason: "an item carries the whole sub-plan written there (#68)",
			emit:   true,
		},
		{
			name:   "nested shape without a sibling type",
			doc:    `{"not": {"properties": {"a": {"type": "string"}}, "additionalProperties": false}}`,
			reason: "the shape survives as a kind-guarded predicate (#72)",
			emit:   true,
		},
		{
			name: "nested SoundOverApproximation",
			doc: `{"not": {"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Kitten"}],
				"discriminator": {"propertyName": "petType"}}, ` + subsetPetDefs + `}`,
			reason: "an asserted discriminator trusts a declared tag, so the nested plan accepts more than the schema",
			emit:   false,
		},
		{
			name: "nested DeclaredIncomplete",
			doc: `{"not": {"type": "object", "properties": {"a": {"not": {"$ref": "#/$defs/Cat"}}}}, ` +
				subsetPetDefs + `}`,
			reason: "the field's own negation is dropped, so nothing in the nested plan rejects what it should",
			emit:   false,
		},
		{
			name:   "nested UnsupportedConversion",
			doc:    `{"not": {"anyOf": [true, {"properties": {"foo": true}}], "unevaluatedProperties": false}}`,
			reason: "unevaluatedProperties is not modeled, so the nested plan accepts more than the schema",
			emit:   false,
		},
		{
			name:   "nested reference is never proven exact",
			doc:    `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {"type": "string"}}}`,
			reason: "a reference's exactness does not consult its target, so it cannot be trusted here",
			emit:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNormalized(t, tt.doc)

			if tt.emit {
				require.Equal(t, 1, countNegations(t, got.Plan), tt.reason)
				require.Equal(t, plan.PredicateDispatch, got.Plan.Capability, tt.reason)
				require.Equal(t, plan.ExactWithValidation, got.Exactness, tt.reason)
			} else {
				require.Zero(t, countNegations(t, got.Plan), tt.reason)
				require.Less(t, got.Plan.Capability, plan.PredicateDispatch,
					"a dropped negation costs nothing at runtime")
				require.Equal(t, plan.DeclaredIncomplete, got.Exactness,
					"nothing left in the plan rejects what the dropped negation would have (#84)")
			}
			require.True(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
		})
	}
}
