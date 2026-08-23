package schemacompiler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func interpret(t *testing.T, p plan.CompilationPlan, instance string) bool {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(instance), &v))
	verdict, err := planterp.Interpret(p, v)
	require.NoError(t, err)
	return verdict.Accepted
}

// TestCompileDecimalCountKeyword pins issue #74: a JSON number is an integer whenever its
// value is one, so `maxLength: 2.0` bounds the string at 2 — it used to arrive as a bound
// of 0 and reject every non-empty string, an under-approximation design §24 forbids.
func TestCompileDecimalCountKeyword(t *testing.T) {
	for _, spelling := range []string{"2", "2.0", "2e0"} {
		t.Run(spelling, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(),
				[]byte(`{"type": "string", "maxLength": `+spelling+`}`), schemacompiler.Options{})
			require.NoError(t, err)
			require.Empty(t, res.Diagnostics)

			require.True(t, interpret(t, res.Plan, `"f"`))
			require.True(t, interpret(t, res.Plan, `"fo"`))
			require.False(t, interpret(t, res.Plan, `"foo"`))
		})
	}
}

// TestCompileInvalidCountKeyword pins the other half: a value that is no non-negative
// integer makes the schema invalid, and the compiler drops the keyword rather than guess a
// bound. Dropping only widens what is accepted, and nothing residual closes the gap, so the
// keyword is reported as [plan.DiagnosticUnenforced] — not [plan.DiagnosticAssumed], whose
// excess is bounded by the plan's own machinery.
func TestCompileInvalidCountKeyword(t *testing.T) {
	for _, spelling := range []string{"2.5", "-1", `"2"`} {
		t.Run(spelling, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(),
				[]byte(`{"type": "string", "maxLength": `+spelling+`}`), schemacompiler.Options{})
			require.NoError(t, err)
			require.Len(t, res.Diagnostics, 1)
			d := res.Diagnostics[0]
			require.Equal(t, plan.DiagnosticUnenforced, d.Kind)
			require.Equal(t, plan.SeverityError, d.Severity)
			require.Equal(t, "/maxLength", d.Pointer)
			require.True(t, strings.Contains(d.Message, "maxLength"), d.Message)
			require.True(t, strings.Contains(d.Message, "non-negative integer"), d.Message)

			require.True(t, interpret(t, res.Plan, `"foo"`))
		})
	}
}
