package gogen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestTypeName(t *testing.T) {
	for _, tt := range []struct {
		pointer string
		want    string
	}{
		{"/components/schemas/Pet", "Pet"},
		{"/$defs/Pet", "Pet"},
		{"/definitions/Pet", "Pet"},

		// Container keywords drop out; the author's own segments stay.
		{"/components/schemas/Pet/properties/address", "PetAddress"},
		{"/components/schemas/Pet/properties/address/properties/city", "PetAddressCity"},
		{"/$defs/Pet/dependentSchemas/kind", "PetKind"},

		// The four aliased positions.
		{"/components/schemas/Pet/properties/tags/items", "PetTagsItem"},
		{"/components/schemas/Pet/prefixItems/0", "PetItem0"},
		{"/components/schemas/Pet/oneOf/1", "PetVariant1"},
		{"/components/schemas/Pet/anyOf/0", "PetVariant0"},
		{"/components/schemas/Pet/allOf/0", "PetMember0"},

		// Every other keyword names itself.
		{"/components/schemas/Pet/not", "PetNot"},
		{"/components/schemas/Pet/propertyNames", "PetPropertyNames"},
		{"/components/schemas/Pet/additionalProperties", "PetAdditionalProperties"},

		// Separators split; nothing else is interpreted.
		{"/$defs/io.k8s.api.core.v1.Pod", "IoK8sApiCoreV1Pod"},
		{"/$defs/pet-tag", "PetTag"},
		{"/$defs/pet_tag", "PetTag"},

		// No initialism table and no pluralization: the author's spelling survives.
		{"/$defs/petId", "PetId"},
		{"/$defs/HTTPServer", "HTTPServer"},

		// Upper-casing the first word escapes every Go keyword, since all of them are
		// lower-case: a schema named `type` needs no annotation.
		{"/components/schemas/type", "Type"},
	} {
		t.Run(tt.pointer, func(t *testing.T) {
			got, err := gogen.TypeName(tt.pointer)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestTypeNameRefuses pins the restrictive half: a name the rule cannot derive is an error
// naming the pointer, never a sanitized guess. Inventing `Type2FA` from `2FA` would put a
// name in the author's code that they did not write and cannot predict.
func TestTypeNameRefuses(t *testing.T) {
	for _, tt := range []struct{ name, pointer string }{
		{"leading digit", "/components/schemas/2FA"},
		{"slash in a property name", "/components/schemas/Pet/properties/a~1b"},
		{"pattern is not a name", "/components/schemas/Pet/patternProperties/^a.*$"},
		{"punctuation", "/components/schemas/Pet!"},
		{"no nameable segment", "/components/schemas"},
		{"root", "/"},
		{"empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gogen.TypeName(tt.pointer)
			require.Error(t, err)
			require.Contains(t, err.Error(), gogen.NameExtension)
		})
	}
}

func plans(entries map[string]plan.Metadata) map[plan.SchemaID]plan.CompilationPlan {
	out := make(map[plan.SchemaID]plan.CompilationPlan, len(entries))
	for ptr, meta := range entries {
		out[plan.SchemaID(ptr)] = plan.CompilationPlan{
			Representation: &plan.AnyRepresentation{},
			Metadata:       meta,
		}
	}
	return out
}

func TestAssign(t *testing.T) {
	names, err := gogen.Assign(plans(map[string]plan.Metadata{
		"/components/schemas/Pet":                    {},
		"/components/schemas/Pet/properties/address": {},
		"/components/schemas/order":                  {Extensions: map[string]any{gogen.NameExtension: "PurchaseOrder"}},
		// Title is metadata, never a name.
		"/components/schemas/Tag": {Title: "A Tag Of Some Kind"},
	}))
	require.NoError(t, err)
	require.Equal(t, gogen.Names{
		"/components/schemas/Pet":                    "Pet",
		"/components/schemas/Pet/properties/address": "PetAddress",
		"/components/schemas/order":                  "PurchaseOrder",
		"/components/schemas/Tag":                    "Tag",
	}, names)
}

// TestAssignCollides pins the rule that costs an author work: two schemas deriving one
// name stop the build. A `Pet2` suffix would mean adding a schema renames a type in code
// its author does not own, and the rename would be invisible in their diff.
func TestAssignCollides(t *testing.T) {
	_, err := gogen.Assign(plans(map[string]plan.Metadata{
		"/components/schemas/PetAddress":             {},
		"/components/schemas/Pet/properties/address": {},
	}))
	require.Error(t, err)

	var collision *gogen.CollisionError
	require.ErrorAs(t, err, &collision)
	require.Equal(t, "PetAddress", collision.Name)
	require.Equal(t, []plan.SchemaID{
		"/components/schemas/Pet/properties/address",
		"/components/schemas/PetAddress",
	}, collision.Pointers)

	// An override on either side resolves it.
	_, err = gogen.Assign(plans(map[string]plan.Metadata{
		"/components/schemas/PetAddress":             {},
		"/components/schemas/Pet/properties/address": {Extensions: map[string]any{gogen.NameExtension: "PetHome"}},
	}))
	require.NoError(t, err)
}

// TestAssignReportsEveryFailure: fixing one name can reveal the next, so an author gets
// the whole list rather than one round-trip per bad name.
func TestAssignReportsEveryFailure(t *testing.T) {
	_, err := gogen.Assign(plans(map[string]plan.Metadata{
		"/components/schemas/2FA": {},
		"/components/schemas/-":   {},
		"/components/schemas/ok":  {},
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "/components/schemas/2FA")
	require.Contains(t, err.Error(), "/components/schemas/-")
}

func TestAssignRejectsBadOverride(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"not a string", 42},
		{"not an identifier", "Pet Address"},
		{"go keyword", "range"},
		{"empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gogen.Assign(plans(map[string]plan.Metadata{
				"/components/schemas/Pet": {Extensions: map[string]any{gogen.NameExtension: tt.value}},
			}))
			require.Error(t, err)
		})
	}
}
