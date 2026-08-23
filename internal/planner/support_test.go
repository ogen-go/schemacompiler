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
			want:  &plan.FullyResolved{},
		},
		{
			name:  "a nil part contributes nothing",
			parts: []plan.ResolutionPlan{nil, &plan.FullyResolved{}},
			want:  &plan.FullyResolved{},
		},
		{
			name: "static definitions union",
			parts: []plan.ResolutionPlan{
				&plan.StaticReferenceGraph{Definitions: defs},
				&plan.StaticReferenceGraph{Definitions: other},
			},
			want: &plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
				"/$defs/S": {}, "/$defs/T": {},
			}},
		},
		{
			name: "one dynamic part makes the whole plan dynamic",
			parts: []plan.ResolutionPlan{
				&plan.StaticReferenceGraph{Definitions: defs},
				&plan.DynamicReferenceGraph{DynamicAnchors: map[string][]plan.SchemaID{"a": {"/$defs/T"}}},
			},
			want: &plan.DynamicReferenceGraph{
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

// TestMergeResolutionHandlesEveryVariant pins the guard added for issue #63. The
// unhandled branch panics rather than returning FullyResolved, which would be the claim
// being falsified — a backend told to emit no reference machinery for a plan that needs it.
//
// The panic cannot be provoked from here any more, and that is the point: since #133 the
// variants carry pointer receivers, so ResolutionPlan is sealed for real. A value spelling
// no longer satisfies the interface, and a fourth variant can only come from
// plan/resolution.go. What used to reach the default branch — `&plan.StaticReferenceGraph{}`
// against value receivers — is now the ordinary spelling and is handled.
func TestMergeResolutionHandlesEveryVariant(t *testing.T) {
	for _, p := range []plan.ResolutionPlan{
		nil,
		&plan.FullyResolved{},
		&plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{"/$defs/S": {}}},
		&plan.DynamicReferenceGraph{DynamicAnchors: map[string][]plan.SchemaID{"a": {"/$defs/S"}}},
	} {
		require.NotPanics(t, func() { mergeResolution(p) }, "%T", p)
	}
}
