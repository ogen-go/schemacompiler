package nodetree

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func guarded(e plan.PredicateExpr) plan.GuardedPredicate {
	return plan.GuardedPredicate{Applicability: plan.SetObject, Expression: e}
}

func structureOf(required ...string) *plan.ObjectStructurePredicate {
	s := &plan.ObjectStructurePredicate{}
	for _, n := range required {
		s.Properties = append(s.Properties, plan.PropertyCheck{Name: n, Presence: plan.PresenceRequired})
	}
	return s
}

// TestWithoutRestatedRequired pins when the restatement may be dropped. The negative rows
// are what keeps it sound: a required name the structure does not require, or requires
// under a different guard, is the only thing rejecting that instance.
func TestWithoutRestatedRequired(t *testing.T) {
	optional := plan.ObjectStructurePredicate{
		Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceOptional}},
	}

	for _, tt := range []struct {
		name  string
		preds []plan.GuardedPredicate
		want  int
	}{
		{
			name:  "the structure requires the same name",
			preds: []plan.GuardedPredicate{guarded(structureOf("a")), guarded(&plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name:  "the structure requires a superset",
			preds: []plan.GuardedPredicate{guarded(structureOf("a", "b")), guarded(&plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name:  "the structure requires only some of the names",
			preds: []plan.GuardedPredicate{guarded(structureOf("a")), guarded(&plan.RequiredPredicate{Properties: []string{"a", "b"}})},
			want:  2,
		},
		{
			name:  "the structure declares the name but does not require it",
			preds: []plan.GuardedPredicate{guarded(&optional), guarded(&plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  2,
		},
		{
			name:  "there is no structure at all",
			preds: []plan.GuardedPredicate{guarded(&plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name: "the structure guards a different kind",
			preds: []plan.GuardedPredicate{
				{Applicability: plan.SetAny, Expression: structureOf("a")},
				guarded(&plan.RequiredPredicate{Properties: []string{"a"}}),
			},
			want: 2,
		},
		{
			name: "the structure is an assertion, not a guard",
			preds: []plan.GuardedPredicate{
				{Applicability: plan.SetObject, Assert: true, Expression: structureOf("a")},
				guarded(&plan.RequiredPredicate{Properties: []string{"a"}}),
			},
			want: 2,
		},
		{
			name: "the required check is an assertion, so it also rejects a non-object",
			preds: []plan.GuardedPredicate{
				guarded(structureOf("a")),
				{Applicability: plan.SetObject, Assert: true, Expression: &plan.RequiredPredicate{Properties: []string{"a"}}},
			},
			want: 2,
		},
		{
			name: "two restatements are both dropped",
			preds: []plan.GuardedPredicate{
				guarded(structureOf("a", "b")),
				guarded(&plan.RequiredPredicate{Properties: []string{"a"}}),
				guarded(&plan.RequiredPredicate{Properties: []string{"b"}}),
			},
			want: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]plan.GuardedPredicate(nil), tt.preds...)
			f := newPlanFusion(tt.preds)
			kept := 0
			for _, skip := range f.skip {
				if !skip {
					kept++
				}
			}
			require.Equal(t, tt.want, kept)
			require.Equal(t, before, tt.preds, "the input plan must not be modified in place")
		})
	}
}

// TestPlanFusionFoldsCountBounds pins when a count bound is folded into the structure
// beside it. Folding is only sound when that structure walks exactly the instances the
// bound applies to, so the negative rows are the ones doing the work.
func TestPlanFusionFoldsCountBounds(t *testing.T) {
	objectStruct := guarded(structureOf())
	arrayStruct := plan.GuardedPredicate{
		Applicability: plan.SetArray,
		Expression:    &plan.ArrayStructurePredicate{},
	}
	maxItems := plan.GuardedPredicate{
		Applicability: plan.SetArray,
		Expression:    &plan.MaxItemsPredicate{Value: 10},
	}

	for _, tt := range []struct {
		name   string
		preds  []plan.GuardedPredicate
		object countBounds
		array  countBounds
		kept   int
	}{
		{
			name:   "minProperties folds into the object structure",
			preds:  []plan.GuardedPredicate{objectStruct, guarded(&plan.MinPropertiesPredicate{Value: 2})},
			object: countBounds{min: 2, hasMin: true},
			kept:   1,
		},
		{
			name: "both object bounds fold",
			preds: []plan.GuardedPredicate{
				objectStruct,
				guarded(&plan.MinPropertiesPredicate{Value: 2}),
				guarded(&plan.MaxPropertiesPredicate{Value: 5}),
			},
			object: countBounds{min: 2, max: 5, hasMin: true, hasMax: true},
			kept:   1,
		},
		{
			name:  "maxItems folds into the array structure",
			preds: []plan.GuardedPredicate{arrayStruct, maxItems},
			array: countBounds{max: 10, hasMax: true},
			kept:  1,
		},
		{
			name:  "no structure to fold into",
			preds: []plan.GuardedPredicate{maxItems},
			kept:  1,
		},
		{
			name: "the structure guards a different kind",
			preds: []plan.GuardedPredicate{
				{Applicability: plan.SetAny, Expression: &plan.ArrayStructurePredicate{}},
				maxItems,
			},
			kept: 2,
		},
		{
			name: "the bound is an assertion, so it also rejects a non-array",
			preds: []plan.GuardedPredicate{
				arrayStruct,
				{Applicability: plan.SetArray, Assert: true, Expression: &plan.MaxItemsPredicate{Value: 10}},
			},
			kept: 2,
		},
		{
			name: "two structures leave no single walk to fold into",
			preds: []plan.GuardedPredicate{
				arrayStruct,
				{Applicability: plan.SetArray, Expression: &plan.ArrayStructurePredicate{}},
				maxItems,
			},
			kept: 3,
		},
		{
			name: "a second bound of the same side is not silently dropped",
			preds: []plan.GuardedPredicate{
				arrayStruct,
				maxItems,
				{Applicability: plan.SetArray, Expression: &plan.MaxItemsPredicate{Value: 3}},
			},
			array: countBounds{max: 10, hasMax: true},
			kept:  2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newPlanFusion(tt.preds)
			kept := 0
			for _, skip := range f.skip {
				if !skip {
					kept++
				}
			}
			require.Equal(t, tt.kept, kept)
			require.Equal(t, tt.object, f.objectBounds)
			require.Equal(t, tt.array, f.arrayBounds)
		})
	}
}
