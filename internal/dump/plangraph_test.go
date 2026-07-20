package dump_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	schemacompiler "github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestPlanDOT_KindDisjointOneOf(t *testing.T) {
	result, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"oneOf": [{"type": "string"}, {"type": "number"}]}`), schemacompiler.Options{})
	require.NoError(t, err)

	var out strings.Builder
	dump.PlanDOT(&out, result.Plan, nil)
	got := out.String()

	require.Contains(t, got, "digraph plan {")
	require.Contains(t, got, "static-dispatch")
	require.Contains(t, got, `label="string"`)
	require.Contains(t, got, `label="number"`)
	require.Contains(t, got, "style=solid")
}

func TestPlanDOT_RefAndStub(t *testing.T) {
	result, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"$defs": {"Named": {"type": "string"}}, "$ref": "#/$defs/Named"}`), schemacompiler.Options{})
	require.NoError(t, err)

	defs, ok := result.Plan.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok)

	var out strings.Builder
	dump.PlanDOT(&out, result.Plan, defs.Definitions)
	got := out.String()
	require.Contains(t, got, `label="ref:/$defs/Named [direct-go-type]"`)
	require.Contains(t, got, "style=dashed")
	require.Contains(t, got, `label="string [direct-go-type]"`)

	// A reference target absent from defs renders a stub node instead of recursing.
	out.Reset()
	dump.PlanDOT(&out, result.Plan, nil)
	got = out.String()
	require.Contains(t, got, `label="?/$defs/Named"`)
}
