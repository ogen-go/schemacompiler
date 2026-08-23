package nodetree

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func guarded(e plan.PredicateExpr) plan.GuardedPredicate {
	return plan.GuardedPredicate{Applicability: plan.SetObject, Expression: e}
}

func structureOf(required ...string) plan.ObjectStructurePredicate {
	s := plan.ObjectStructurePredicate{}
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
			preds: []plan.GuardedPredicate{guarded(structureOf("a")), guarded(plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name:  "the structure requires a superset",
			preds: []plan.GuardedPredicate{guarded(structureOf("a", "b")), guarded(plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name:  "the structure requires only some of the names",
			preds: []plan.GuardedPredicate{guarded(structureOf("a")), guarded(plan.RequiredPredicate{Properties: []string{"a", "b"}})},
			want:  2,
		},
		{
			name:  "the structure declares the name but does not require it",
			preds: []plan.GuardedPredicate{guarded(optional), guarded(plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  2,
		},
		{
			name:  "there is no structure at all",
			preds: []plan.GuardedPredicate{guarded(plan.RequiredPredicate{Properties: []string{"a"}})},
			want:  1,
		},
		{
			name: "the structure guards a different kind",
			preds: []plan.GuardedPredicate{
				{Applicability: plan.SetAny, Expression: structureOf("a")},
				guarded(plan.RequiredPredicate{Properties: []string{"a"}}),
			},
			want: 2,
		},
		{
			name: "the structure is an assertion, not a guard",
			preds: []plan.GuardedPredicate{
				{Applicability: plan.SetObject, Assert: true, Expression: structureOf("a")},
				guarded(plan.RequiredPredicate{Properties: []string{"a"}}),
			},
			want: 2,
		},
		{
			name: "the required check is an assertion, so it also rejects a non-object",
			preds: []plan.GuardedPredicate{
				guarded(structureOf("a")),
				{Applicability: plan.SetObject, Assert: true, Expression: plan.RequiredPredicate{Properties: []string{"a"}}},
			},
			want: 2,
		},
		{
			name: "two restatements are both dropped",
			preds: []plan.GuardedPredicate{
				guarded(structureOf("a", "b")),
				guarded(plan.RequiredPredicate{Properties: []string{"a"}}),
				guarded(plan.RequiredPredicate{Properties: []string{"b"}}),
			},
			want: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]plan.GuardedPredicate(nil), tt.preds...)
			got := withoutRestatedRequired(tt.preds)
			require.Len(t, got, tt.want)
			require.Equal(t, before, tt.preds, "the input plan must not be modified in place")
		})
	}
}
