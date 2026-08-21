package schemacompiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

// parentIdiomDoc is the classic OpenAPI polymorphism spelling of OAS 3.0.3 line 2761: Pet
// carries the discriminator and each child includes it via `allOf`.
const parentIdiomDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Pet:
      type: object
      required: [petType]
      properties:
        petType: {type: string}
      discriminator:
        propertyName: petType
        mapping:
          cat: '#/components/schemas/Cat'
          dog: '#/components/schemas/Dog'
    Cat:
      allOf:
        - $ref: '#/components/schemas/Pet'
        - type: object
          properties: {hunts: {type: boolean}}
    Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
        - type: object
          properties: {packSize: {type: integer}}
`

// orphanParentDoc declares the same parent with no schema including it via `allOf`, so a
// reverse lookup would find no alternates at all.
const orphanParentDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Pet:
      type: object
      required: [petType]
      properties:
        petType: {type: string}
      discriminator:
        propertyName: petType
    Cat:
      type: object
      properties: {hunts: {type: boolean}}
`

// unusedDiscriminatorMessage is the stable head of the diagnostic under test.
const unusedDiscriminatorMessage = "`discriminator` does not produce a tagged union here"

func diagnosticsAt(diags []plan.Diagnostic, pointer string) []plan.Diagnostic {
	var out []plan.Diagnostic
	for _, d := range diags {
		if d.Pointer == pointer {
			out = append(out, d)
		}
	}
	return out
}

func requireOneMessageContaining(t *testing.T, diags []plan.Diagnostic, substr string) plan.Diagnostic {
	t.Helper()

	var found []plan.Diagnostic
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			found = append(found, d)
		}
	}
	require.Len(t, found, 1, "diagnostics: %v", diags)
	return found[0]
}

// TestCompileDocument_DiscriminatorOnAllOfParent pins issue #46: the parent idiom compiles
// without a tagged union, but no longer silently — the schema that declared the
// discriminator carries a warning naming the reason.
func TestCompileDocument_DiscriminatorOnAllOfParent(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, parentIdiomDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	require.IsType(t, plan.NoDispatch{}, res.Plans["/components/schemas/Pet"].Dispatch)

	d := requireOneMessageContaining(t, res.Diagnostics, unusedDiscriminatorMessage)
	require.Equal(t, "/components/schemas/Pet", d.Pointer)
	require.Equal(t, plan.SeverityWarning, d.Severity)
	require.Contains(t, d.Message, `"petType"`)

	for _, child := range []string{"/components/schemas/Cat", "/components/schemas/Dog"} {
		for _, cd := range diagnosticsAt(res.Diagnostics, child) {
			require.NotContains(t, cd.Message, unusedDiscriminatorMessage,
				"only the schema that declared the discriminator is diagnosed")
		}
	}
}

// TestCompileDocument_DiscriminatorOnParentWithoutChildren checks that the diagnostic does
// not depend on children existing: a parent nothing includes is reported the same way.
func TestCompileDocument_DiscriminatorOnParentWithoutChildren(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, orphanParentDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	require.IsType(t, plan.NoDispatch{}, res.Plans["/components/schemas/Pet"].Dispatch)

	d := requireOneMessageContaining(t, res.Diagnostics, unusedDiscriminatorMessage)
	require.Equal(t, "/components/schemas/Pet", d.Pointer)
	require.Equal(t, plan.SeverityWarning, d.Severity)
}

// TestCompile_UnusedDiscriminator pins the single-schema path: a declaration the compiler
// cannot dispatch on is reported there too, not only when compiling a whole document.
func TestCompile_UnusedDiscriminator(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{
			name: "declared alongside allOf",
			schema: `{
				"allOf": [{"$ref": "#/$defs/Cat"}],
				"discriminator": {"propertyName": "petType"},
				"$defs": {"Cat": {"type": "object", "required": ["petType"],
					"properties": {"petType": {"const": "cat"}}}}
			}`,
		},
		{
			name: "plain object",
			schema: `{
				"type": "object",
				"required": ["petType"],
				"properties": {"petType": {"type": "string"}},
				"discriminator": {"propertyName": "petType"}
			}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)

			require.IsType(t, plan.NoDispatch{}, res.Plan.Dispatch)
			d := requireOneMessageContaining(t, res.Diagnostics, unusedDiscriminatorMessage)
			require.Equal(t, "", d.Pointer)
			require.Equal(t, plan.SeverityWarning, d.Severity)
		})
	}
}

// TestCompile_DeclaredDiscriminatorOnUnionIsNotReportedUnused guards the diagnostic's
// scope: a `discriminator` that does drive dispatch must not be reported as unused.
func TestCompile_DeclaredDiscriminatorOnUnionIsNotReportedUnused(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(), []byte(`{
		"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
		"discriminator": {"propertyName": "petType"},
		"$defs": {
			"Cat": {"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}}},
			"Dog": {"type": "object", "required": ["petType"], "properties": {"petType": {"const": "dog"}}}
		}
	}`), schemacompiler.Options{})
	require.NoError(t, err)

	require.IsType(t, plan.PropertyDispatch{}, res.Plan.Dispatch)
	for _, d := range res.Diagnostics {
		require.NotContains(t, d.Message, "discriminator", "the declaration drove dispatch")
	}
}
