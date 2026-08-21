package schemacompiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

// requireExactnessAgrees enforces the invariant design §24/§25 imply: a result whose
// capability is past PredicateDispatch cannot also claim its Go type is exact.
func requireExactnessAgrees(t *testing.T, capability plan.CapabilityLevel, exactness plan.Exactness) {
	t.Helper()
	if capability >= plan.EvaluationStateValidation {
		require.Equal(t, plan.UnsupportedConversion, exactness,
			"capability %d must not advertise exactness %d", capability, exactness)
	}
}

func TestCompileCapabilityExactnessAgree(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
		want   plan.CapabilityLevel
	}{
		{
			name: "definition references a missing target",
			schema: `{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}},
				"$defs":{"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/Nope"}}}}}`,
			want: plan.Unsupported,
		},
		{
			name:   "root property references a missing target",
			schema: `{"type":"object","properties":{"a":{"$ref":"#/$defs/Nope"}}}`,
			want:   plan.Unsupported,
		},
		{
			name:   "root is a missing reference",
			schema: `{"$ref":"#/$defs/Nope"}`,
			want:   plan.Unsupported,
		},
		{
			name: "definition references an expensive target",
			schema: `{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}},
				"$defs":{"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/B"}}},
					"B":{"type":"object","unevaluatedProperties":false}}}`,
			want: plan.EvaluationStateValidation,
		},
		{
			name:   "plain object stays exact",
			schema: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			want:   plan.DirectGoType,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.want, res.Capability)
			require.Equal(t, res.Capability, res.Plan.Capability, "Result mirrors the root plan")
			requireExactnessAgrees(t, res.Capability, res.Exactness)
		})
	}
}

// A dangling reference anywhere, root included, must be reported against the plan that
// cannot be generated, with a source position like every other diagnostic.
func TestCompileRootDanglingRefDiagnostic(t *testing.T) {
	const schema = `type: object
properties:
  a: {$ref: '#/$defs/Nope'}
`
	res, err := schemacompiler.Compile(context.Background(), []byte(schema),
		schemacompiler.Options{BaseURI: "schema.yml"})
	require.NoError(t, err)
	require.Equal(t, plan.Unsupported, res.Capability)
	require.Equal(t, plan.UnsupportedConversion, res.Exactness)

	var named bool
	for _, d := range res.Diagnostics {
		require.False(t, d.Position.IsZero(), "diagnostic without a position: %+v", d)
		if d.Severity == plan.SeverityError && strings.Contains(d.Message, "no compiled schema") {
			named = true
		}
	}
	require.True(t, named, "expected a diagnostic naming the ungeneratable plan: %+v", res.Diagnostics)
}

// TestCompileDeclaredIncompleteIsGeneratable pins issue #84: a dropped constraint reports
// DeclaredIncomplete, and it does so at a capability a backend still generates — the one
// combination in which the capability gate and the exactness ladder deliberately disagree
// (docs/integration.md §6).
func TestCompileDeclaredIncompleteIsGeneratable(t *testing.T) {
	for _, tt := range []struct {
		name       string
		schema     string
		capability plan.CapabilityLevel
		exactness  plan.Exactness
	}{
		{
			name:       "negation over an exactly modeled operand keeps its residual check",
			schema:     `{"not":{"type":"integer"}}`,
			capability: plan.PredicateDispatch,
			exactness:  plan.SoundOverApproximation,
		},
		{
			name:       "negation over an object operand is dropped and nothing closes the gap",
			schema:     `{"not":{"type":"object","properties":{"a":{"type":"string","minLength":1}}}}`,
			capability: plan.DirectGoType,
			exactness:  plan.DeclaredIncomplete,
		},
		{
			name:       "negation over a reference operand is dropped",
			schema:     `{"not":{"$ref":"#/$defs/S"},"$defs":{"S":{"type":"string"}}}`,
			capability: plan.DirectGoType,
			exactness:  plan.DeclaredIncomplete,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.capability, res.Capability)
			require.Equal(t, tt.exactness, res.Exactness)
			requireExactnessAgrees(t, res.Capability, res.Exactness)
		})
	}
}

// The ladder is ordered worst-last: every rung a backend may still generate sorts before
// UnsupportedConversion, which is what every ordered comparison against it relies on.
func TestExactnessOrdering(t *testing.T) {
	require.Less(t, plan.ExactPureRepresentation, plan.ExactWithValidation)
	require.Less(t, plan.ExactWithValidation, plan.SoundOverApproximation)
	require.Less(t, plan.SoundOverApproximation, plan.DeclaredIncomplete)
	require.Less(t, plan.DeclaredIncomplete, plan.UnsupportedConversion)
}

func TestCompileDocumentCapabilityExactnessAgree(t *testing.T) {
	for _, tt := range []struct {
		name string
		doc  string
		want plan.CapabilityLevel
	}{
		{name: "dangling target", doc: danglingRefDoc, want: plan.Unsupported},
		{name: "expensive target", doc: capabilityDoc, want: plan.EvaluationStateValidation},
		{name: "plain components", doc: petstoreDoc, want: plan.DirectGoType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
				Schemas: componentSchemas(t, tt.doc),
			}, schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.want, res.Capability)
			requireExactnessAgrees(t, res.Capability, res.Exactness)
		})
	}
}
