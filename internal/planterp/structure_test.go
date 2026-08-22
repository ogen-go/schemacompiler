package planterp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func anyPlan() plan.CompilationPlan {
	return plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.FullyResolved{},
	}
}

// stringPlan is a plan accepting only strings, stated entirely in validation.
func stringPlan() plan.CompilationPlan {
	p := anyPlan()
	p.Validation = plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
		{Applicability: plan.SetString, Assert: true},
	}}
	return p
}

// checking wraps e as the whole of a plan's validation, over an Any representation, so a
// verdict can only have come from the check itself (design §4.1).
func checking(e plan.PredicateExpr, guard plan.KindSet) plan.CompilationPlan {
	p := anyPlan()
	p.Validation = plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
		{Applicability: guard, Expression: e},
	}}
	return p
}

func TestObjectStructurePredicate(t *testing.T) {
	never := plan.CompilationPlan{
		Representation: plan.NeverRepresentation{},
		Validation:     plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{Applicability: 0, Assert: true}}},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.FullyResolved{},
	}

	tests := []struct {
		name      string
		predicate plan.ObjectStructurePredicate
		value     any
		accepted  bool
	}{
		{
			name: "a required property must be present",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceRequired},
			}},
			value:    map[string]any{},
			accepted: false,
		},
		{
			name: "an optional property may be absent",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceOptional},
			}},
			value:    map[string]any{},
			accepted: true,
		},
		{
			name: "a present property is checked against its plan",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceOptional},
			}},
			value:    map[string]any{"a": float64(1)},
			accepted: false,
		},
		{
			name: "a nullable property admits null",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceOptional, Nullable: true},
			}},
			value:    map[string]any{"a": nil},
			accepted: true,
		},
		{
			name: "a non-nullable property does not",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceOptional},
			}},
			value:    map[string]any{"a": nil},
			accepted: false,
		},
		{
			name: "an undeclared name falls to a matching pattern",
			predicate: plan.ObjectStructurePredicate{Patterns: []plan.PatternCheck{
				{Pattern: "^x", Plan: stringPlan()},
			}},
			value:    map[string]any{"xy": float64(1)},
			accepted: false,
		},
		{
			name: "a non-matching name does not reach the pattern",
			predicate: plan.ObjectStructurePredicate{Patterns: []plan.PatternCheck{
				{Pattern: "^x", Plan: stringPlan()},
			}},
			value:    map[string]any{"zy": float64(1)},
			accepted: true,
		},
		{
			name: "a declared name is not also run through a matching pattern",
			predicate: plan.ObjectStructurePredicate{
				Properties: []plan.PropertyCheck{{Name: "xy", Plan: anyPlan(), Presence: plan.PresenceOptional}},
				Patterns:   []plan.PatternCheck{{Pattern: "^x", Plan: stringPlan()}},
			},
			value:    map[string]any{"xy": float64(1)},
			accepted: true,
		},
		{
			name:      "additionalProperties false rejects an undeclared name",
			predicate: plan.ObjectStructurePredicate{Additional: &never},
			value:     map[string]any{"z": float64(1)},
			accepted:  false,
		},
		{
			name:      "a nil Additional admits it",
			predicate: plan.ObjectStructurePredicate{},
			value:     map[string]any{"z": float64(1)},
			accepted:  true,
		},
		{
			name: "the guard excuses a non-object",
			predicate: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: stringPlan(), Presence: plan.PresenceRequired},
			}},
			value:    "not an object",
			accepted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := planterp.Interpret(checking(tt.predicate, plan.SetObject), tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.accepted, v.Accepted, "reason: %v", v.Reason)
		})
	}
}

func TestArrayStructurePredicate(t *testing.T) {
	restAny := anyPlan()

	tests := []struct {
		name      string
		predicate plan.ArrayStructurePredicate
		value     any
		accepted  bool
	}{
		{
			name:      "a prefix element is checked against its own plan",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{stringPlan()}, Rest: &restAny},
			value:     []any{float64(1)},
			accepted:  false,
		},
		{
			name:      "and accepted when it fits",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{stringPlan()}, Rest: &restAny},
			value:     []any{"a"},
			accepted:  true,
		},
		{
			name:      "elements past the prefix go to Rest",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{anyPlan()}, Rest: &[]plan.CompilationPlan{stringPlan()}[0]},
			value:     []any{float64(1), float64(2)},
			accepted:  false,
		},
		{
			name:      "a nil Rest rejects anything past the prefix",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{anyPlan()}},
			value:     []any{float64(1), float64(2)},
			accepted:  false,
		},
		{
			name:      "and admits an array that stops at the prefix",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{anyPlan()}},
			value:     []any{float64(1)},
			accepted:  true,
		},
		{
			name:      "the guard excuses a non-array",
			predicate: plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{stringPlan()}},
			value:     float64(1),
			accepted:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := planterp.Interpret(checking(tt.predicate, plan.SetArray), tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.accepted, v.Accepted, "reason: %v", v.Reason)
		})
	}
}

// TestReferencePredicate pins that a `$ref` is checked from the validation plan
// (issue #115): the referring plan's representation is Any, so the verdict can only have
// come from resolving the name against the document's graph.
func TestReferencePredicate(t *testing.T) {
	root := checking(plan.ReferencePredicate{Name: "#/$defs/S"}, plan.SetAny)
	root.Resolution = plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
		"#/$defs/S": stringPlan(),
	}}

	t.Run("the target accepts", func(t *testing.T) {
		v, err := planterp.Interpret(root, "a")
		require.NoError(t, err)
		require.True(t, v.Accepted, "reason: %v", v.Reason)
	})

	t.Run("the target rejects", func(t *testing.T) {
		v, err := planterp.Interpret(root, float64(1))
		require.NoError(t, err)
		require.False(t, v.Accepted)
	})

	t.Run("an unresolvable name is an internal error, never an acceptance", func(t *testing.T) {
		dangling := checking(plan.ReferencePredicate{Name: "#/$defs/missing"}, plan.SetAny)
		dangling.Resolution = plan.StaticReferenceGraph{}
		_, err := planterp.Interpret(dangling, "a")
		require.Error(t, err)
	})

	t.Run("a cycle with no instance descent is an internal error", func(t *testing.T) {
		loop := checking(plan.ReferencePredicate{Name: "#/$defs/L"}, plan.SetAny)
		loop.Resolution = plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
			"#/$defs/L": checking(plan.ReferencePredicate{Name: "#/$defs/L"}, plan.SetAny),
		}}
		_, err := planterp.Interpret(loop, "a")
		require.Error(t, err)
	})
}
