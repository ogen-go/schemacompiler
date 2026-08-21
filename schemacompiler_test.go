package schemacompiler_test

import (
	"context"
	"net/url"
	"strings"
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

func TestCompileUninhabitedSchemaDiagnostic(t *testing.T) {
	// Required self-recursion is representable/guarded but has no finite instance; Compile
	// surfaces a SeverityWarning so a generator does not emit a dead recursive type (#8).
	schema := `{
		"$defs": {
			"A": {"type": "object", "required": ["self"], "properties": {"self": {"$ref": "#/$defs/A"}}}
		},
		"$ref": "#/$defs/A"
	}`
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	var sawUninhabited bool
	for _, d := range res.Diagnostics {
		if d.Severity == plan.SeverityWarning && strings.Contains(d.Message, "uninhabited") {
			sawUninhabited = true
		}
	}
	require.True(t, sawUninhabited, "expected an uninhabited-schema warning; got %+v", res.Diagnostics)
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

func TestCompileDiagnosticPositions(t *testing.T) {
	const schema = `type: object
properties:
  broken:
    $ref: '#/$defs/Missing'
  dyn:
    $dynamicRef: '#meta'
`
	res, err := schemacompiler.Compile(context.Background(), []byte(schema),
		schemacompiler.Options{BaseURI: "schema.yml"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Diagnostics)

	byPointer := make(map[string]plan.Diagnostic, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		require.False(t, d.Position.IsZero(), "diagnostic without a position: %+v", d)
		byPointer[d.Pointer] = d
	}

	broken, ok := byPointer["/properties/broken"]
	require.True(t, ok, "expected a diagnostic for the dangling $ref: %+v", res.Diagnostics)
	require.Equal(t, plan.Position{File: "schema.yml", Line: 4, Column: 5}, broken.Position)
	require.Contains(t, broken.Message, "unresolved $ref")

	dyn, ok := byPointer["/properties/dyn"]
	require.True(t, ok, "expected a diagnostic for the $dynamicRef: %+v", res.Diagnostics)
	// Planner diagnostics carry the position of the schema being planned — here the
	// document root — rather than a position guessed from their breadcrumb (issue #37).
	require.Equal(t, plan.Position{File: "schema.yml", Line: 1, Column: 1}, dyn.Position)
}

func TestCompileDefinitionDiagnosticPointers(t *testing.T) {
	const schema = `$ref: '#/$defs/A'
$defs:
  A:
    type: object
    properties:
      dyn:
        $dynamicRef: '#meta'
`
	const baseURI = "https://ex.com/schema.yml"
	res, err := schemacompiler.Compile(context.Background(), []byte(schema),
		schemacompiler.Options{BaseURI: baseURI})
	require.NoError(t, err)

	var found bool
	for _, d := range res.Diagnostics {
		if d.Pointer == "/$defs/A/properties/dyn" {
			found = true
			require.Equal(t, plan.Position{File: baseURI, Line: 4, Column: 5}, d.Position)
		}
	}
	require.True(t, found, "a definition's diagnostic should carry a document-absolute pointer: %+v", res.Diagnostics)
}

func TestCompileDiagnosticPositionsAcrossDocuments(t *testing.T) {
	root := []byte(`{
  "$defs": {
    "Name": {"type": "string"}
  },
  "type": "object",
  "properties": {"n": {"$ref": "other.json#/$defs/Name"}}
}`)
	other := []byte(`{
  "$defs": {
    "Name": {
      "type": "object",
      "unevaluatedProperties": false
    }
  }
}`)
	loader := func(context.Context, *url.URL) ([]byte, error) { return other, nil }

	res, err := schemacompiler.Compile(context.Background(), root, schemacompiler.Options{
		BaseURI: "https://ex.com/root.json",
		Loader:  loader,
	})
	require.NoError(t, err)

	var found bool
	for _, d := range res.Diagnostics {
		if !strings.Contains(d.Message, "unevaluatedProperties") {
			continue
		}
		found = true
		require.Equal(t, "/$defs/Name", d.Pointer)
		require.Equal(t,
			plan.Position{File: "https://ex.com/other.json", Line: 3, Column: 13},
			d.Position,
			"the diagnostic must name the document that actually declares the schema")
	}
	require.True(t, found, "expected an unevaluatedProperties diagnostic: %+v", res.Diagnostics)
}

func TestCompileDiagnosticPointerEscaping(t *testing.T) {
	doc := []byte(`{
  "type": "object",
  "properties": {
    "a/b": {"type": "object", "unevaluatedProperties": false},
    "a~b": {"$dynamicRef": "#meta"}
  }
}`)
	res, err := schemacompiler.Compile(context.Background(), doc,
		schemacompiler.Options{BaseURI: "schema.json"})
	require.NoError(t, err)

	pointers := make(map[string]plan.Position, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		pointers[d.Pointer] = d.Position
	}
	rootPos := plan.Position{File: "schema.json", Line: 1, Column: 1}
	require.Contains(t, pointers, "/properties/a~1b")
	require.Contains(t, pointers, "/properties/a~0b")
	require.Equal(t, rootPos, pointers["/properties/a~1b"])
	require.Equal(t, rootPos, pointers["/properties/a~0b"])
}

func TestCompileDiagnosticPositionMergedBranch(t *testing.T) {
	doc := []byte(`{
  "type": "object",
  "properties": {
    "x": {"type": "object"}
  },
  "allOf": [
    {"properties": {"x": {"type": "object", "unevaluatedProperties": false}}}
  ]
}`)
	res, err := schemacompiler.Compile(context.Background(), doc,
		schemacompiler.Options{BaseURI: "schema.json"})
	require.NoError(t, err)

	var found bool
	for _, d := range res.Diagnostics {
		if !strings.Contains(d.Message, "unevaluatedProperties") {
			continue
		}
		found = true
		// The finding belongs to the allOf branch, not to the sibling "/properties/x"
		// the breadcrumb collides with: report the planned schema, never that line.
		require.Equal(t, "/properties/x", d.Pointer)
		require.Equal(t, plan.Position{File: "schema.json", Line: 1, Column: 1}, d.Position)
	}
	require.True(t, found, "expected an unevaluatedProperties diagnostic: %+v", res.Diagnostics)
}
