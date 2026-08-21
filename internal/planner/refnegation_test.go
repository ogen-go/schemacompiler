package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestBuild_NegationOverReferenceConsultsTarget covers issue #108: negating a `$ref` is
// decided by the target's own plan, one row per rung of [plan.Exactness] the target can
// land on, plus the cases the walk must refuse to answer — recursion, a cycle, and a
// dangling target.
//
// The JSON-Schema-Test-Suite corpus reaches none of this: no suite schema negates a `$ref`
// at all, so the differential oracle is byte-identical with and without the change.
func TestBuild_NegationOverReferenceConsultsTarget(t *testing.T) {
	const unsupported = `{"anyOf": [true, {"properties": {"foo": true}}], "unevaluatedProperties": false}`

	for _, tt := range []struct {
		name   string
		doc    string
		reason string
		emit   bool
	}{
		{
			name:   "target is ExactPureRepresentation",
			doc:    `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {"type": "string"}}}`,
			reason: "the target's representation alone reproduces its schema",
			emit:   true,
		},
		{
			name:   "target is ExactWithValidation",
			doc:    `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {"type": "string", "minLength": 3}}}`,
			reason: "the target's residual validator closes its representation's gap",
			emit:   true,
		},
		{
			name: "target is SoundOverApproximation",
			doc: `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Dog", "dog": "#/$defs/Cat"}}},
				"Cat": {"type": "object", "required": ["petType", "name"],
					"properties": {"petType": {"type": "string"}, "name": {"type": "string"}}},
				"Dog": {"type": "object", "required": ["petType", "bark"],
					"properties": {"petType": {"type": "string"}, "bark": {"type": "boolean"}}}}}`,
			reason: "the target trusts a declared tag instead of proving its branches disjoint",
			emit:   false,
		},
		{
			name: "target is DeclaredIncomplete",
			doc: `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {"type": "object",
				"properties": {"a": {"not": ` + unsupported + `}}}}}`,
			reason: "the target's own negation is dropped, so nothing in it rejects what it should",
			emit:   false,
		},
		{
			name:   "target is UnsupportedConversion",
			doc:    `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": ` + unsupported + `}}`,
			reason: "the target is not modeled at all",
			emit:   false,
		},
		{
			name: "target is guarded-recursive",
			doc: `{"not": {"$ref": "#/$defs/S"}, "$defs": {"S": {"type": "object",
				"properties": {"next": {"$ref": "#/$defs/S"}}}}}`,
			reason: "a recursive target is a knot, not a value this gate can judge (design §19)",
			emit:   false,
		},
		{
			name: "target is in a reference cycle",
			doc: `{"not": {"$ref": "#/$defs/A"}, "$defs": {
				"A": {"type": "object", "properties": {"b": {"$ref": "#/$defs/B"}}},
				"B": {"type": "object", "properties": {"a": {"$ref": "#/$defs/A"}}}}}`,
			reason: "the walk must terminate, and it does so pessimistically",
			emit:   false,
		},
		{
			name: "target is exact several hops away",
			doc: `{"not": {"$ref": "#/$defs/A"}, "$defs": {
				"A": {"$ref": "#/$defs/B"}, "B": {"$ref": "#/$defs/C"},
				"C": {"type": "string", "minLength": 3}}}`,
			reason: "every hop is resolved, and the last one is exact",
			emit:   true,
		},
		{
			name: "target is unsupported several hops away",
			doc: `{"not": {"$ref": "#/$defs/A"}, "$defs": {
				"A": {"$ref": "#/$defs/B"}, "B": {"$ref": "#/$defs/C"},
				"C": ` + unsupported + `}}`,
			reason: "one unmodeled hop disqualifies the whole chain",
			emit:   false,
		},
		{
			name: "target is reached through an object field",
			doc: `{"not": {"type": "object", "properties": {"a": {"$ref": "#/$defs/S"}}},
				"$defs": {"S": ` + unsupported + `}}`,
			reason: "a reference nested inside the operand's plan is judged the same way",
			emit:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNormalized(t, tt.doc)

			if tt.emit {
				require.NotZero(t, countNegations(t, got.Plan), tt.reason)
				require.Equal(t, plan.ExactWithValidation, got.Exactness, tt.reason)
			} else {
				require.Zero(t, countNegations(t, got.Plan), tt.reason)
				require.Equal(t, plan.DeclaredIncomplete, got.Exactness, tt.reason)
			}
			require.True(t, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)
		})
	}
}
