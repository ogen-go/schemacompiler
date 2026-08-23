package schemacompiler_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestDiagnosticKindVocabulary walks one schema per kind and pins the rung each kind
// implies, because that correspondence is the whole point of the field: a consumer reading
// Kind learns what the diagnostic says about the accepted set without matching on message
// text, and reads the same answer [plan.Exactness] gives for the plan as a whole (§24.1).
func TestDiagnosticKindVocabulary(t *testing.T) {
	const assertedUnion = `{
		"$defs": {
			"A": {"type":"object","required":["t"],"properties":{"t":{"type":"string"},"a":{"type":"string"}}},
			"B": {"type":"object","required":["t"],"properties":{"t":{"type":"string"},"b":{"type":"string"}}}
		},
		"oneOf": [{"$ref":"#/$defs/A"}, {"$ref":"#/$defs/B"}],
		"discriminator": {"propertyName":"t","mapping":{"a":"#/$defs/A","b":"#/$defs/B"}}
	}`

	for _, tt := range []struct {
		name      string
		schema    string
		kind      plan.DiagnosticKind
		severity  plan.Severity
		exactness plan.Exactness
	}{
		{
			name:      "advisory",
			schema:    `{"allOf":[{"type":"string","format":"date-time"},{"format":"uuid"}]}`,
			kind:      plan.DiagnosticAdvisory,
			severity:  plan.SeverityInfo,
			exactness: plan.ExactWithValidation,
		},
		{
			name:      "cost",
			schema:    `{"type":"array","contains":{"type":"string"},"minContains":2}`,
			kind:      plan.DiagnosticCost,
			severity:  plan.SeverityWarning,
			exactness: plan.ExactWithValidation,
		},
		{
			name:      "assumed",
			schema:    assertedUnion,
			kind:      plan.DiagnosticAssumed,
			severity:  plan.SeverityInfo,
			exactness: plan.SoundOverApproximation,
		},
		{
			name:      "unenforced",
			schema:    `{"type":"string","maxLength":2.5}`,
			kind:      plan.DiagnosticUnenforced,
			severity:  plan.SeverityError,
			exactness: plan.DeclaredIncomplete,
		},
		{
			name:      "unsupported",
			schema:    `{"type":"object","unevaluatedProperties":false}`,
			kind:      plan.DiagnosticUnsupported,
			severity:  plan.SeverityError,
			exactness: plan.UnsupportedConversion,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := compileString(t, tt.schema)
			require.Equal(t, tt.exactness, res.Exactness)

			var found []plan.Diagnostic
			for _, d := range res.Diagnostics {
				require.NotEqual(t, plan.DiagnosticUnclassified, d.Kind, "unclassified: %s", d.Message)
				if d.Kind == tt.kind {
					found = append(found, d)
				}
			}
			require.Len(t, found, 1, "diagnostics: %v", res.Diagnostics)
			require.Equal(t, tt.severity, found[0].Severity)
		})
	}
}

// TestDiagnosticKindSurvivesExactSchema keeps the vocabulary from being satisfied by a
// compiler that classifies everything: a schema converted exactly says nothing at all.
func TestDiagnosticKindSurvivesExactSchema(t *testing.T) {
	res := compileString(t, `{"type":"string"}`)
	require.Empty(t, res.Diagnostics)
	require.Equal(t, plan.ExactPureRepresentation, res.Exactness)
}
