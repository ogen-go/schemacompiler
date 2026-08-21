package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunDiagnosticsDefaultBaseURI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	const schema = `{
  "type": "object",
  "properties": {
    "dyn": {"$dynamicRef": "#meta"}
  }
}`
	require.NoError(t, os.WriteFile(path, []byte(schema), 0o600))

	var out strings.Builder
	require.NoError(t, run([]string{"-diagnostics", path}, &out))

	require.Contains(t, out.String(), path+":1:1 (/properties/dyn)")
}

func TestDefaultBaseURI(t *testing.T) {
	require.Equal(t, "schema.json", defaultBaseURI([]string{"schema.json"}))
	require.Empty(t, defaultBaseURI(nil))
}
