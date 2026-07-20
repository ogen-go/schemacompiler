package schemacompiler_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/go-faster/errors"
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

func TestCompileExternalRefWithLoader(t *testing.T) {
	// An external $ref resolves via Options.Loader, so the document compiles without any
	// unresolved-ref error diagnostics.
	const otherURI = "https://ex.com/other.json"
	loader := func(_ context.Context, u *url.URL) ([]byte, error) {
		if u.String() != otherURI {
			return nil, errors.Errorf("unexpected fetch %q", u)
		}
		return []byte(`{"$defs": {"Name": {"type": "string", "minLength": 1}}}`), nil
	}

	res, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"$ref": "other.json#/$defs/Name"}`),
		schemacompiler.Options{BaseURI: "https://ex.com/root.json", Loader: loader})
	require.NoError(t, err)
	for _, d := range res.Diagnostics {
		require.NotEqual(t, plan.SeverityError, d.Severity, "unexpected error diagnostic: %s", d.Message)
	}
}

func TestCompileExternalRefWithoutLoader(t *testing.T) {
	// With no loader, the external $ref degrades to an error diagnostic.
	res, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"$ref": "https://ex.com/other.json#/$defs/Name"}`),
		schemacompiler.Options{})
	require.NoError(t, err)

	var sawError bool
	for _, d := range res.Diagnostics {
		if d.Severity == plan.SeverityError {
			sawError = true
		}
	}
	require.True(t, sawError, "expected unresolved-ref error diagnostic without a loader")
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
