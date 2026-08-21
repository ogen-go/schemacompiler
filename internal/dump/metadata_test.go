package dump_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlan_Metadata(t *testing.T) {
	got := compilePlan(t, `{
		"type": "string",
		"title": "Name",
		"description": "A name.",
		"deprecated": true,
		"writeOnly": true,
		"default": "anon",
		"examples": ["a"],
		"xml": {"name": "Name", "attribute": true},
		"x-ogen-name": "Nm"
	}`)
	require.Contains(t, got, `title="Name"`)
	require.Contains(t, got, `description="A name."`)
	require.Contains(t, got, "deprecated=true")
	require.Contains(t, got, "writeOnly=true")
	require.Contains(t, got, `default="anon"`)
	require.Contains(t, got, `example[0]="a"`)
	require.Contains(t, got, `xml name="Name" namespace="" prefix="" attribute=true wrapped=false`)
	require.Contains(t, got, `extension "x-ogen-name"="Nm"`)
}

func TestPlan_FieldMetadata(t *testing.T) {
	got := compilePlan(t, `{
		"type": "object",
		"properties": {"id": {"type": "string", "title": "Identifier", "x-ogen-type": "uuid.UUID"}}
	}`)
	require.Contains(t, got, `title="Identifier"`)
	require.Contains(t, got, `extension "x-ogen-type"="uuid.UUID"`)
}

func TestPlan_ExtensionOrderIsDeterministic(t *testing.T) {
	const schema = `{"type": "string", "x-c": 3, "x-a": 1, "x-b": {"z": 1, "y": 2}}`
	got := compilePlan(t, schema)
	require.Contains(t, got, "extension \"x-a\"=1\n")
	require.Contains(t, got, "extension \"x-b\"={\"y\":2,\"z\":1}\n")
	require.Contains(t, got, "extension \"x-c\"=3\n")
	require.Less(t, strings.Index(got, `"x-a"`), strings.Index(got, `"x-b"`))
	require.Less(t, strings.Index(got, `"x-b"`), strings.Index(got, `"x-c"`))
	for range 8 {
		require.Equal(t, got, compilePlan(t, schema))
	}
}
