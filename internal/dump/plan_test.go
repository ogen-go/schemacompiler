package dump_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	schemacompiler "github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/dump"
)

func compilePlan(t *testing.T, schema string) string {
	t.Helper()
	result, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	var out strings.Builder
	dump.Plan(&out, result.Plan)
	return out.String()
}

func TestPlan_ObjectWithRef(t *testing.T) {
	got := compilePlan(t, `{"$defs": {"Named": {"type": "string"}}, "$ref": "#/$defs/Named"}`)
	require.Contains(t, got, `Reference "/$defs/Named"`)
	require.Contains(t, got, "StaticReferenceGraph")
	require.Contains(t, got, `definition "/$defs/Named"`)
	require.Contains(t, got, "Primitive string")
}

func TestPlan_KindDisjointOneOf(t *testing.T) {
	got := compilePlan(t, `{"oneOf": [{"type": "string"}, {"type": "number"}]}`)
	require.Contains(t, got, "KindDispatch")
	require.Contains(t, got, "case string")
	require.Contains(t, got, "case number")
}

func TestPlan_Enum(t *testing.T) {
	got := compilePlan(t, `{"enum": ["red", "green", "blue"]}`)
	require.Contains(t, got, "LiteralDispatch")
	require.Contains(t, got, `case "red"`)
	require.Contains(t, got, `case "green"`)
	require.Contains(t, got, `case "blue"`)
}
