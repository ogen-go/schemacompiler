package frontend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURI(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"schema.yml", "/schema.yml"},
		{"./schema.yml", "/schema.yml"},
		{"a/b.yml", "/a/b.yml"},
		{"/schema.yml", "/schema.yml"},
		{"https://ex.test/schema.yml", "https://ex.test/schema.yml"},
		{"file:///tmp/schema.yml", "file:///tmp/schema.yml"},
		{"urn:example:schema", "urn:example:schema"},
	} {
		got, err := normalizeBaseURI(tt.raw)
		require.NoError(t, err, tt.raw)
		require.Equal(t, tt.want, got, tt.raw)
	}
}

// A relative retrieval URI must resolve in-document references, not dangle them
// (issue #28): registration and lookup have to agree on one key.
func TestRelativeBaseURIResolvesRefs(t *testing.T) {
	const doc = `{
		"type": "object",
		"properties": {"a": {"$ref": "#/$defs/A"}},
		"$defs": {"A": {"type": "string"}}
	}`

	for _, baseURI := range []string{"", "schema.yml", "./schema.yml", "/schema.yml", "https://ex.test/schema.yml"} {
		t.Run(baseURI, func(t *testing.T) {
			s, err := Load(t.Context(), []byte(doc), baseURI)
			require.NoError(t, err)
			require.Empty(t, s.Unresolved)
			require.Equal(t, baseURI, s.Root.Position.File, "positions keep the caller's URI")
		})
	}
}
