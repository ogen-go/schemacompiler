package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// patternedObject is {"type":"object","properties":{name:{"type":"string"}},
// "patternProperties":{pattern:{"type":"string","minLength":1}}}.
func patternedObject(name, pattern string) ir.Expr {
	return ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{
			Properties:        []ir.PropertyExpr{{Name: name, Schema: ir.Kinds{Set: plan.SetString}}},
			PatternProperties: []ir.PatternPropertyExpr{{Pattern: pattern, Schema: constrainedString()}},
		}},
	}}
}

// TestConstraintsFor_ECMASemantics pins issue #111: whether a `patternProperties` pattern
// covers a declared property name is decided in ECMA-262, the language JSON Schema writes
// the pattern in, not in RE2.
//
// Both cases below reach the generated type. Under RE2 the first silently emits a field
// without the pattern schema's constraint while still claiming exactness, and the second
// drops the constraint loudly. Under ECMA-262 the pattern covers the name in both, so the
// field carries `minLength` and the plan stays exact.
func TestConstraintsFor_ECMASemantics(t *testing.T) {
	tests := []struct {
		name     string
		property string
		pattern  string
	}{
		{
			// RE2's \s is [\t\n\f\r ]; ECMA-262's also covers U+00A0.
			name:     "whitespace class RE2 reads narrowly",
			property: " ",
			pattern:  `^\s$`,
		},
		{
			// RE2 cannot compile lookbehind, so it could only drop the pattern.
			name:     "lookbehind RE2 cannot compile",
			property: "ab",
			pattern:  `(?<=a)b`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(patternedObject(tt.property, tt.pattern), nil)

			obj, ok := got.Plan.Representation.(plan.ObjectRepresentation)
			require.True(t, ok, "got %T", got.Plan.Representation)

			field := plannerField(t, obj, tt.property).Plan
			require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, field.Representation)
			require.Len(t, field.ResidualChecks(), 1,
				"the pattern schema must be intersected into the field it covers")
			require.Equal(t, plan.MinLengthPredicate{Value: 1}, field.ResidualChecks()[0].Expression)

			require.Equal(t, plan.ExactWithValidation, got.Exactness)
			require.Empty(t, got.Diagnostics)
		})
	}
}

// TestConstraintsFor_UndecidablePatternDrops pins the sound fallback: a pattern neither
// engine can decide leaves it unknown whether it covers the name, so the pattern schema is
// dropped rather than assumed either way, and the plan says so (issue #84).
//
// `\p{Letter}` is the one direction where RE2 was the more permissive engine — it compiles
// the long property name and ECMA-262 does not — so this is a constraint the move gives up.
func TestConstraintsFor_UndecidablePatternDrops(t *testing.T) {
	got := planner.Build(patternedObject("a", `\p{Letter}`), nil)

	obj, ok := got.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", got.Plan.Representation)

	field := plannerField(t, obj, "a").Plan
	require.Empty(t, field.ResidualChecks(),
		"an undecidable pattern must not be intersected in")

	require.Equal(t, plan.DeclaredIncomplete, got.Exactness)
	require.NotEmpty(t, got.Diagnostics)
}
