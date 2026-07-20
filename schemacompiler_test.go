package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestCompile(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		capability plan.CapabilityLevel
		exactness  plan.Exactness
	}{
		{
			name:       "direct string",
			schema:     `{"type": "string"}`,
			capability: plan.DirectGoType,
			exactness:  plan.ExactPureRepresentation,
		},
		{
			name:       "string with validation",
			schema:     `{"type": "string", "minLength": 3}`,
			capability: plan.GoTypeWithValidation,
			exactness:  plan.ExactWithValidation,
		},
		{
			name:       "kind-disjoint oneOf",
			schema:     `{"oneOf": [{"type": "string"}, {"type": "number"}]}`,
			capability: plan.StaticDispatch,
		},
		{
			name:       "dynamicRef is unsupported in v1",
			schema:     `{"$dynamicRef": "#meta"}`,
			capability: plan.DynamicSchemaResolution,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tt.capability, res.Capability, "capability")
			if tt.exactness != 0 || tt.capability == plan.DirectGoType {
				require.Equal(t, tt.exactness, res.Exactness, "exactness")
			}
		})
	}
}

func TestCompileDynamicRefDiagnostic(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"$dynamicRef": "#meta"}`), schemacompiler.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, res.Diagnostics, "expected a diagnostic for $dynamicRef")

	var sawError bool
	for _, d := range res.Diagnostics {
		if d.Severity == plan.SeverityError {
			sawError = true
		}
	}
	require.True(t, sawError, "expected a SeverityError diagnostic")
}
