package planner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestMergeResolution pins the join over the resolution lattice: the result must demand
// whatever any part demands, because under-reporting resolution cost leaves a backend
// emitting no reference machinery for a plan that needs it (design §24).
func TestMergeResolution(t *testing.T) {
	defs := map[plan.SchemaID]plan.CompilationPlan{"/$defs/S": {}}
	other := map[plan.SchemaID]plan.CompilationPlan{"/$defs/T": {}}

	for _, tt := range []struct {
		name  string
		parts []plan.ResolutionPlan
		want  plan.ResolutionPlan
	}{
		{
			name:  "nothing to merge",
			parts: nil,
			want:  plan.FullyResolved{},
		},
		{
			name:  "a nil part contributes nothing",
			parts: []plan.ResolutionPlan{nil, plan.FullyResolved{}},
			want:  plan.FullyResolved{},
		},
		{
			name: "static definitions union",
			parts: []plan.ResolutionPlan{
				plan.StaticReferenceGraph{Definitions: defs},
				plan.StaticReferenceGraph{Definitions: other},
			},
			want: plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
				"/$defs/S": {}, "/$defs/T": {},
			}},
		},
		{
			name: "one dynamic part makes the whole plan dynamic",
			parts: []plan.ResolutionPlan{
				plan.StaticReferenceGraph{Definitions: defs},
				plan.DynamicReferenceGraph{DynamicAnchors: map[string][]plan.SchemaID{"a": {"/$defs/T"}}},
			},
			want: plan.DynamicReferenceGraph{
				StaticDefinitions: defs,
				DynamicAnchors:    map[string][]plan.SchemaID{"a": {"/$defs/T"}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeResolution(tt.parts...))
		})
	}
}

// TestMergeResolutionPanicsOnAnUnhandledVariant pins issue #63. The interface is sealed by
// an unexported method, so a fourth variant can only come from plan/resolution.go — but
// the receivers are values, which puts isResolutionPlan in the pointer's method set too.
// A pointer to one of the three therefore satisfies the interface, matches no case, and
// used to return FullyResolved: the definitions were silently discarded and the plan
// claimed it needed no reference machinery at all.
func TestMergeResolutionPanicsOnAnUnhandledVariant(t *testing.T) {
	pointer := &plan.StaticReferenceGraph{
		Definitions: map[plan.SchemaID]plan.CompilationPlan{"/$defs/S": {}},
	}

	require.PanicsWithValue(t,
		"planner: unhandled plan.ResolutionPlan variant *plan.StaticReferenceGraph",
		func() { mergeResolution(pointer) },
		"an unhandled variant must not be silently downgraded to FullyResolved")

	require.NotPanics(t, func() { mergeResolution(*pointer) }, "the value form is handled")
}
