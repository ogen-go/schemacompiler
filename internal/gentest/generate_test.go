package gentest

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite types.go from schema.json")

func TestUpdate(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate types.go")
	}
	require.NoError(t, os.WriteFile("types.go", generated(t), 0o600))
}
