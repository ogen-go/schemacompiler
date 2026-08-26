package dump_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const unsortedProperties = `{
	"type": "object",
	"properties": {
		"zeta": {"type": "string"},
		"alpha": {"type": "string"},
		"mid": {"type": "string"}
	}
}`

// TestPlan_FieldsAreDumpedInSourceOrder pins that the dump follows
// plan.ObjectRepresentation.Fields rather than sorting it (issue #89): the order is the
// information the plan now carries, and sorting would hide it.
func TestPlan_FieldsAreDumpedInSourceOrder(t *testing.T) {
	got := compilePlan(t, unsortedProperties)

	var names []string
	for line := range strings.SplitSeq(got, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, `field "`); ok {
			name, _, _ := strings.Cut(after, `"`)
			names = append(names, name)
		}
	}
	require.Equal(t, []string{"zeta", "alpha", "mid"}, names)
}

// TestPlan_IsDeterministic keeps the dump byte-identical across runs of the same plan:
// Go randomizes map iteration, so a consumer that ranged over a keyed Fields emitted
// different text per run (issue #89).
func TestPlan_IsDeterministic(t *testing.T) {
	want := compilePlan(t, unsortedProperties)
	for range 32 {
		require.Equal(t, want, compilePlan(t, unsortedProperties))
	}
}
