package schemacompiler_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestDiagnosticKindVocabulary walks one schema per kind and pins whether it means the
// plan accepts more than its schema, because that is the whole point of the field: since
// §25.1 retired the Exactness ladder, Kind is the only machine-readable answer to §24.1's
// question, and it is per-construct where the ladder was per-plan.
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
		name        string
		schema      string
		kind        plan.DiagnosticKind
		severity    plan.Severity
		overAccepts bool
	}{
		{
			name:     "advisory",
			schema:   `{"allOf":[{"type":"string","format":"date-time"},{"format":"uuid"}]}`,
			kind:     plan.DiagnosticAdvisory,
			severity: plan.SeverityInfo,
		},
		{
			name:     "cost",
			schema:   `{"type":"array","contains":{"type":"string"},"minContains":2}`,
			kind:     plan.DiagnosticCost,
			severity: plan.SeverityWarning,
		},
		{
			name:        "assumed",
			schema:      assertedUnion,
			kind:        plan.DiagnosticAssumed,
			severity:    plan.SeverityInfo,
			overAccepts: true,
		},
		{
			name:        "unenforced",
			schema:      `{"type":"string","maxLength":2.5}`,
			kind:        plan.DiagnosticUnenforced,
			severity:    plan.SeverityError,
			overAccepts: true,
		},
		{
			name:        "unsupported",
			schema:      `{"type":"object","unevaluatedProperties":false}`,
			kind:        plan.DiagnosticUnsupported,
			severity:    plan.SeverityError,
			overAccepts: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := compileString(t, tt.schema)
			require.Equal(t, tt.overAccepts, overAccepts(res))

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
	require.False(t, overAccepts(res))
}
