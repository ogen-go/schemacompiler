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
			exactness:  plan.ExactWithValidation,
		},
		{
			name:       "negation over an object operand keeps its residual check",
			schema:     `{"not":{"type":"object","properties":{"a":{"type":"string","minLength":1}}}}`,
			capability: plan.PredicateDispatch,
			exactness:  plan.ExactWithValidation,
		},
		{
			name:       "negation over a reference to an exact target keeps its residual check",
			schema:     `{"not":{"$ref":"#/$defs/S"},"$defs":{"S":{"type":"string"}}}`,
			capability: plan.PredicateDispatch,
			exactness:  plan.ExactWithValidation,
		},
		{
			name:       "negation over an unmodeled operand is dropped and nothing closes the gap",
			schema:     `{"not":{"anyOf":[true,{"properties":{"foo":true}}],"unevaluatedProperties":false}}`,
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

// TestCompileExactnessTracksTheAcceptedSet pins issue #95: [plan.Exactness] describes the
// accepted set of the whole plan, so a representation wider than the schema costs nothing
// as long as the residual validator closes the gap. A rung is spent only when the plan
// still accepts more after validation runs.
func TestCompileExactnessTracksTheAcceptedSet(t *testing.T) {
	const assertedDiscriminator = `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "kind", "mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}},
		"$defs": {
			"Cat": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}}},
			"Dog": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}}}
		}
	}`

	for _, tt := range []struct {
		name      string
		schema    string
		exactness plan.Exactness
		why       string
	}{
		{
			name:      "bare minLength widens the representation to any",
			schema:    `{"minLength":3}`,
			exactness: plan.ExactWithValidation,
			why:       "the kind-guarded MinLength accepts every non-string and rejects short strings",
		},
		{
			name:      "bare properties/additionalProperties widens the representation to any",
			schema:    `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			exactness: plan.ExactWithValidation,
			why:       "the kind-guarded Shape carries the whole object plan and is vacuous elsewhere",
		},
		{
			name:      "match-count dispatch is a cost, not an approximation",
			schema:    `{"contains":{"const":1},"minContains":2}`,
			exactness: plan.ExactWithValidation,
			why:       "PredicateCountDispatch's lowering contract reproduces the accepted set exactly",
		},
		{
			name:      "a negation over an exactly modeled operand is exact",
			schema:    `{"not":{"type":"integer"}}`,
			exactness: plan.ExactWithValidation,
			why:       "negating an exact plan yields an exact plan (issue #82)",
		},
		{
			name:      "an asserted discriminator still over-accepts",
			schema:    assertedDiscriminator,
			exactness: plan.SoundOverApproximation,
			why:       "nothing proved the branches disjoint, so a mis-tagged instance is accepted",
		},
		{
			name:      "a negation over a reference to an exact target is exact",
			schema:    `{"not":{"$ref":"#/$defs/S"},"$defs":{"S":{"type":"string"}}}`,
			exactness: plan.ExactWithValidation,
			why:       "the target is resolved and its plan reproduces its schema (issue #108)",
		},
		{
			name:      "a dropped negation is closed by nothing",
			schema:    `{"not":{"anyOf":[true,{"properties":{"foo":true}}],"unevaluatedProperties":false}}`,
			exactness: plan.DeclaredIncomplete,
			why:       "the negation was dropped and no residual check replaces it (issue #84)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.exactness, res.Exactness, tt.why)
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
