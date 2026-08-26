package conformance

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// suiteOptionalEnv opts out of the hard failure below, for a checkout that genuinely
// cannot initialize the submodule.
const suiteOptionalEnv = "SCHEMACOMPILER_SUITE_OPTIONAL"

// suiteFiles returns every JSON-Schema-Test-Suite file under [suiteRoot], sorted.
//
// A missing or empty submodule fails the test rather than skipping it. The suite is the
// corpus behind the differential oracle, and a skip reads as a pass: a run that silently
// checks zero schemas reports the same green as one that checks every schema, which is
// how a harness rots. Set SCHEMACOMPILER_SUITE_OPTIONAL=1 to downgrade to a skip.
func suiteFiles(t *testing.T) []string {
	t.Helper()

	optional := os.Getenv(suiteOptionalEnv) != ""
	absent := func(format string, args ...any) {
		t.Helper()
		if optional {
			t.Skipf(format, args...)
		}
		args = append(args, suiteOptionalEnv)
		t.Fatalf(format+"; set %s=1 to skip instead", args...)
	}

	info, err := os.Stat(suiteRoot)
	if err != nil || !info.IsDir() {
		absent("JSON-Schema-Test-Suite submodule not present at %s; "+
			"run `git submodule update --init testdata/JSON-Schema-Test-Suite`", suiteRoot)
	}

	var files []string
	walkErr := filepath.Walk(suiteRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() && strings.HasSuffix(p, ".json") {
			files = append(files, p)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(files) == 0 {
		absent("JSON-Schema-Test-Suite submodule present at %s but empty", suiteRoot)
	}

	slices.Sort(files)
	return files
}
