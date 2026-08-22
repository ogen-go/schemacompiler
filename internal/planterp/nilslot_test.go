package planterp_test

import (
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestUnfilledSlots pins what an unpopulated slot means in each structural predicate.
//
// Two slots express absence deliberately, and both are pointers:
// [plan.ObjectStructurePredicate.Additional] nil means anything is admitted, and
// [plan.ArrayStructurePredicate.Rest] nil means nothing past the prefix is. Everywhere
// else a slot the planner never filled in would evaluate as a zero plan, which accepts
// every instance — a silent widening where design §24 wants a loud failure. So it is an
// [planterp.InternalError], which cannot be mistaken for a verdict.
func TestUnfilledSlots(t *testing.T) {
	str := leaf(plan.PrimitiveRepresentation{Kind: plan.KindString})
	anything := leaf(plan.AnyRepresentation{})

	tests := []struct {
		name string
		pred plan.PredicateExpr
		// internal is the expected InternalError, empty when the absence is deliberate
		// and the plan still reaches a verdict.
		internal string
		value    any
		accept   bool
	}{
		{
			name: "a nil Additional cannot reject",
			pred: plan.ObjectStructurePredicate{Properties: []plan.PropertyCheck{
				{Name: "a", Plan: str, Presence: plan.PresenceOptional},
			}},
			value:  map[string]any{"b": 1.0},
			accept: true,
		},
		{
			name:   "a nil Rest rejects items past the prefix",
			pred:   plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{str}},
			value:  []any{"a", "b"},
			accept: false,
		},
		{
			name: "an unfilled property plan",
			pred: plan.ObjectStructurePredicate{
				Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceRequired}},
				Additional: &anything,
			},
			value:    map[string]any{},
			internal: `planterp: property check "a" at the instance root has no plan`,
		},
		{
			name: "an unfilled pattern plan",
			pred: plan.ObjectStructurePredicate{
				Patterns:   []plan.PatternCheck{{Pattern: "^a"}},
				Additional: &anything,
			},
			value:    map[string]any{"ab": 1.0},
			internal: `planterp: pattern check 0 ("^a") at the instance root has no plan`,
		},
		{
			name:     "an unfilled prefix plan",
			pred:     plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{{}}},
			value:    []any{"a"},
			internal: "planterp: prefix check 0 at the instance root has no plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := plan.SetObject
			if _, isArray := tt.pred.(plan.ArrayStructurePredicate); isArray {
				guard = plan.SetArray
			}
			v, err := planterp.Interpret(checking(tt.pred, guard), tt.value)
			if tt.internal == "" {
				require.NoError(t, err)
				require.Equal(t, tt.accept, v.Accepted)
				return
			}

			var internal *planterp.InternalError
			require.ErrorAs(t, err, &internal)
			require.Equal(t, tt.internal, internal.Error())

			var validate *planterp.ValidateError
			var invalid *planterp.InvalidValueError
			require.False(t, errors.As(err, &validate), "a malformed plan is not a rejection")
			require.False(t, errors.As(err, &invalid))
		})
	}
}

// TestUnfilledSlotLocationIsReported keeps the instance location on a malformed slot: one
// plan node is reached once per sub-value, so the path is what makes it findable.
func TestUnfilledSlotLocationIsReported(t *testing.T) {
	inner := checking(plan.ObjectStructurePredicate{
		Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceRequired}},
	}, plan.SetObject)
	p := checking(plan.ArrayStructurePredicate{Rest: &inner}, plan.SetArray)

	_, err := planterp.Interpret(p, []any{map[string]any{}})

	var internal *planterp.InternalError
	require.ErrorAs(t, err, &internal)
	require.Equal(t, `planterp: property check "a" at /0 has no plan`, internal.Error())
}
