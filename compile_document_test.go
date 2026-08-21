package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

const petstoreDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        child: {$ref: '#/components/schemas/Node'}
        pet: {$ref: '#/components/schemas/Pet'}
    Pet:
      type: object
      title: A pet
      x-ogen-name: PetType
      properties:
        name: {type: string, description: pet name}
`

func componentSchemas(t *testing.T, doc string) *orderedmap.Map[string, *base.SchemaProxy] {
	t.Helper()

	cfg := datamodel.NewDocumentConfiguration()
	cfg.TransformSiblingRefs = false
	cfg.MergeReferencedProperties = false
	cfg.SkipExternalRefResolution = true

	d, err := libopenapi.NewDocumentWithConfiguration([]byte(doc), cfg)
	require.NoError(t, err)

	model, errs := d.BuildV3Model()
	require.NoError(t, errs)
	return model.Model.Components.Schemas
}

func TestCompileDocument(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, petstoreDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	for _, d := range res.Diagnostics {
		require.NotEqual(t, plan.SeverityError, d.Severity, d.Message)
	}

	node, ok := res.Plans["/components/schemas/Node"]
	require.True(t, ok)
	pet, ok := res.Plans["/components/schemas/Pet"]
	require.True(t, ok)
	require.Equal(t, plan.DirectGoType, pet.Capability)

	obj, ok := node.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "Node should be an object, got %T", node.Representation)
	field, ok := obj.Fields["pet"]
	require.True(t, ok)
	ref, ok := field.Plan.Representation.(plan.ReferenceRepresentation)
	require.True(t, ok, "pet should be a reference, got %T", field.Plan.Representation)
	_, inDoc := res.Plans[plan.SchemaID(ref.Name)]
	require.True(t, inDoc, "reference %q must name a compiled plan", ref.Name)
}

func TestCompileDocumentMetadata(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, petstoreDoc),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	pet, ok := res.Plans["/components/schemas/Pet"]
	require.True(t, ok)
	require.Equal(t, "A pet", pet.Metadata.Title)
	require.Equal(t, "PetType", pet.Metadata.Extensions["x-ogen-name"])

	obj, ok := pet.Representation.(plan.ObjectRepresentation)
	require.True(t, ok)
	require.Equal(t, "pet name", obj.Fields["name"].Metadata.Description)
}

func TestCompileDocumentEmpty(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{}, schemacompiler.Options{})
	require.NoError(t, err)
	require.Empty(t, res.Plans)
	require.Empty(t, res.Diagnostics)
}

func TestCompileSchema(t *testing.T) {
	sp, ok := componentSchemas(t, petstoreDoc).Get("Pet")
	require.True(t, ok)

	res, err := schemacompiler.CompileSchema(context.Background(), sp, schemacompiler.Options{})
	require.NoError(t, err)
	require.Equal(t, plan.DirectGoType, res.Capability)

	obj, ok := res.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "Pet should be an object, got %T", res.Plan.Representation)
	require.Contains(t, obj.Fields, "name")
}

func TestCompileSchemaNil(t *testing.T) {
	_, err := schemacompiler.CompileSchema(context.Background(), nil, schemacompiler.Options{})
	require.Error(t, err)
}
