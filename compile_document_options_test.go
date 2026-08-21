package schemacompiler_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

const externalRefDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        pet: {$ref: 'https://ex.test/pet.json'}
`

func petLoader(t *testing.T, calls *int) schemacompiler.Loader {
	t.Helper()
	return func(_ context.Context, uri *url.URL) ([]byte, error) {
		*calls++
		require.Equal(t, "https://ex.test/pet.json", uri.String())
		return []byte(`{"type": "string"}`), nil
	}
}

func requireNoErrorDiagnostics(t *testing.T, diags []plan.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		require.NotEqual(t, plan.SeverityError, d.Severity, d.Message)
	}
}

func TestCompileDocumentLoader(t *testing.T) {
	var calls int
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, externalRefDoc),
	}, schemacompiler.Options{Loader: petLoader(t, &calls)})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "Options.Loader must resolve external refs")
	requireNoErrorDiagnostics(t, res.Diagnostics)
}

func TestCompileSchemaLoader(t *testing.T) {
	sp, ok := componentSchemas(t, externalRefDoc).Get("A")
	require.True(t, ok)

	var calls int
	res, err := schemacompiler.CompileSchema(context.Background(), sp, schemacompiler.Options{
		Loader: petLoader(t, &calls),
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "Options.Loader must resolve external refs")
	requireNoErrorDiagnostics(t, res.Diagnostics)
}

// A relative retrieval URI must not break in-document references (issue #28).
func TestCompileDocumentRelativeBaseURI(t *testing.T) {
	for _, baseURI := range []string{"", "openapi.yml", "./openapi.yml", "/openapi.yml", "https://ex.test/openapi.yml"} {
		t.Run(baseURI, func(t *testing.T) {
			res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
				BaseURI: baseURI,
				Schemas: componentSchemas(t, petstoreDoc),
			}, schemacompiler.Options{})
			require.NoError(t, err)
			requireNoErrorDiagnostics(t, res.Diagnostics)
		})
	}
}

func TestCompileRelativeBaseURI(t *testing.T) {
	const schema = `{
		"type": "object",
		"properties": {"a": {"$ref": "#/$defs/A"}},
		"$defs": {"A": {"type": "string"}}
	}`

	for _, baseURI := range []string{"", "schema.yml", "./schema.yml", "/schema.yml", "https://ex.test/schema.yml"} {
		t.Run(baseURI, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{BaseURI: baseURI})
			require.NoError(t, err)
			requireNoErrorDiagnostics(t, res.Diagnostics)
		})
	}
}

// Document.BaseURI wins over Options.BaseURI; an empty one falls back to it.
func TestCompileDocumentBaseURIPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name string
		doc  string
		opts string
	}{
		{name: "document", doc: "https://ex.test/a.yml", opts: "https://other.test/b.yml"},
		{name: "fallback", doc: "", opts: "https://ex.test/a.yml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string
			_, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
				BaseURI: tt.doc,
				Schemas: componentSchemas(t, relativeExternalRefDoc),
			}, schemacompiler.Options{
				BaseURI: tt.opts,
				Loader: func(_ context.Context, uri *url.URL) ([]byte, error) {
					seen = append(seen, uri.String())
					return []byte(`{"type": "string"}`), nil
				},
			})
			require.NoError(t, err)
			require.Equal(t, []string{"https://ex.test/pet.json"}, seen)
		})
	}
}

const relativeExternalRefDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        pet: {$ref: 'pet.json'}
`
