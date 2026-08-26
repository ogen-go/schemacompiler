package schemacompiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func hasKind(diags []plan.Diagnostic, k plan.DiagnosticKind) bool {
	for _, d := range diags {
		if d.Kind == k {
			return true
		}
	}
	return false
}

// overAccepts reports whether a result says its plan accepts more than its schema, by
// either route §24 admits. It is what a caller writes now that there is no Exactness field
// to compare against: the compiler names the constructs it could not enforce, and the
// caller decides what to do about them (design §25.1).
func overAccepts(res *schemacompiler.Result) bool {
	return hasKind(res.Diagnostics, plan.DiagnosticAssumed) ||
		hasKind(res.Diagnostics, plan.DiagnosticUnenforced) ||
		hasKind(res.Diagnostics, plan.DiagnosticUnsupported)
}

func TestCompileCapabilityRollup(t *testing.T) {
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
			name:   "plain object stays generatable",
			schema: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			want:   plan.DirectGoType,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := compileString(t, tt.schema)
			require.Equal(t, tt.want, res.Capability)
			require.Equal(t, res.Capability, res.Plan.Capability, "Result mirrors the root plan")
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

	var named bool
	for _, d := range res.Diagnostics {
		require.False(t, d.Position.IsZero(), "diagnostic without a position: %+v", d)
		if d.Kind == plan.DiagnosticUnsupported && strings.Contains(d.Message, "no compiled schema") {
			named = true
		}
	}
	require.True(t, named, "expected a diagnostic naming the ungeneratable plan: %+v", res.Diagnostics)
}

// TestCompileUnenforcedIsGeneratable pins issue #84: a dropped constraint is reported, and
// it is reported at a capability a backend still generates from. Incompleteness and cost
// are independent axes — that is why the capability gate cannot stand in for the
// diagnostic, and why the diagnostic cannot stand in for the gate.
func TestCompileUnenforcedIsGeneratable(t *testing.T) {
	for _, tt := range []struct {
		name       string
		schema     string
		capability plan.CapabilityLevel
		unenforced bool
	}{
		{
			name:       "negation over an exactly modeled operand keeps its residual check",
			schema:     `{"not":{"type":"integer"}}`,
			capability: plan.RawEvaluation,
		},
		{
			name:       "negation over an object operand keeps its residual check",
			schema:     `{"not":{"type":"object","properties":{"a":{"type":"string","minLength":1}}}}`,
			capability: plan.RawEvaluation,
		},
		{
			name:       "negation over a reference to an exact target keeps its residual check",
			schema:     `{"not":{"$ref":"#/$defs/S"},"$defs":{"S":{"type":"string"}}}`,
			capability: plan.RawEvaluation,
		},
		{
			name:       "negation over an unmodeled operand is dropped and nothing closes the gap",
			schema:     `{"not":{"anyOf":[true,{"properties":{"foo":true}}],"unevaluatedProperties":false}}`,
			capability: plan.DirectGoType,
			unenforced: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := compileString(t, tt.schema)
			require.Equal(t, tt.capability, res.Capability)
			require.Equal(t, tt.unenforced, hasKind(res.Diagnostics, plan.DiagnosticUnenforced))
		})
	}
}

// TestCompileFidelityTracksTheAcceptedSet pins issue #95: a representation wider than the
// schema is not a loss of fidelity, because §24's contract is the biconditional
// x ⊨ S ⟺ x ∈ ⟦G(S)⟧ ∧ V(S,x) — the residual validator is part of it. Nothing is owed
// until the plan still accepts more after validation runs.
func TestCompileFidelityTracksTheAcceptedSet(t *testing.T) {
	const assertedDiscriminator = `{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "kind", "mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}},
		"$defs": {
			"Cat": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}}},
			"Dog": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}}}
		}
	}`

	for _, tt := range []struct {
		name   string
		schema string
		kind   plan.DiagnosticKind
		why    string
	}{
		{
			name:   "bare minLength widens the representation to any",
			schema: `{"minLength":3}`,
			why:    "the kind-guarded MinLength accepts every non-string and rejects short strings",
		},
		{
			name:   "bare properties/additionalProperties widens the representation to any",
			schema: `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			why:    "the kind-guarded Shape carries the whole object plan and is vacuous elsewhere",
		},
		{
			name:   "match-count dispatch is a cost, not an approximation",
			schema: `{"contains":{"const":1},"minContains":2}`,
			why:    "PredicateCountDispatch's lowering contract reproduces the accepted set exactly",
		},
		{
			name:   "a negation over an exactly modeled operand is exact",
			schema: `{"not":{"type":"integer"}}`,
			why:    "negating an exact plan yields an exact plan (issue #82)",
		},
		{
			name:   "a negation over a reference to an exact target is exact",
			schema: `{"not":{"$ref":"#/$defs/S"},"$defs":{"S":{"type":"string"}}}`,
			why:    "the target is resolved and its plan reproduces its schema (issue #108)",
		},
		{
			name:   "an asserted discriminator still over-accepts",
			schema: assertedDiscriminator,
			kind:   plan.DiagnosticAssumed,
			why:    "nothing proved the branches disjoint, so a mis-tagged instance is accepted",
		},
		{
			name:   "a dropped negation is closed by nothing",
			schema: `{"not":{"anyOf":[true,{"properties":{"foo":true}}],"unevaluatedProperties":false}}`,
			kind:   plan.DiagnosticUnenforced,
			why:    "the negation was dropped and no residual check replaces it (issue #84)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := compileString(t, tt.schema)
			require.Equal(t, tt.kind != plan.DiagnosticUnclassified, overAccepts(res), tt.why)
			if tt.kind != plan.DiagnosticUnclassified {
				require.True(t, hasKind(res.Diagnostics, tt.kind), tt.why)
			}
		})
	}
}

func TestCompileDocumentCapabilityRollup(t *testing.T) {
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
		})
	}
}
