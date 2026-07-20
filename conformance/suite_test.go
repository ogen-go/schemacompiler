// suite_test.go is an opt-in walk of the upstream JSON-Schema-Test-Suite's
// draft2020-12 *schemas* (not its pass/fail instance tests, which don't apply to an
// analysis-only compiler — see corpus_test.go). It never fetches anything: it looks
// for a git submodule this repository does not vendor by default and skips cleanly
// when absent.
package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ogen-go/schemacompiler"
)

// suiteRoot is where the JSON-Schema-Test-Suite submodule would live, relative to
// this package, per docs/implementation.md's package layout.
const suiteRoot = "../testdata/JSON-Schema-Test-Suite/tests/draft2020-12"

// suiteTestCase mirrors one entry of a JSON-Schema-Test-Suite file: {"description":
// ..., "schema": ..., "tests": [...]}. Only description and schema matter here; the
// "tests" (instances + expected valid/invalid) are out of scope for a plan-producing
// compiler.
type suiteTestCase struct {
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// TestJSONSchemaTestSuite walks every *.json file under the draft2020-12 suite and
// asserts Compile never errors or panics on the suite's schemas, logging the
// resulting capability distribution. It requires no network: it skips cleanly when
// the submodule is not checked out. Run explicitly (it is otherwise skipped under
// -short, and always skipped when the corpus is absent):
//
//	git submodule update --init testdata/JSON-Schema-Test-Suite
//	go test ./conformance/... -run TestJSONSchemaTestSuite -v
func TestJSONSchemaTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JSON-Schema-Test-Suite walk in -short mode")
	}
	info, err := os.Stat(suiteRoot)
	if err != nil || !info.IsDir() {
		t.Skipf("JSON-Schema-Test-Suite submodule not present at %s; "+
			"run `git submodule update --init testdata/JSON-Schema-Test-Suite` to opt in", suiteRoot)
	}

	var files []string
	walkErr := filepath.Walk(suiteRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".json") {
			files = append(files, p)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(files) == 0 {
		t.Skip("JSON-Schema-Test-Suite submodule present but empty; nothing to walk")
	}

	dist := make(map[distKey]int)
	var errored, panicked int
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // test-only, path from a controlled walk
		if err != nil {
			t.Errorf("%s: read: %v", f, err)
			continue
		}
		var cases []suiteTestCase
		if err := json.Unmarshal(data, &cases); err != nil {
			// A handful of suite files (e.g. optional/*.json helpers) may not follow
			// the {description, schema, tests} shape; skip rather than fail the walk.
			continue
		}
		for _, c := range cases {
			if len(c.Schema) == 0 {
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked++
						t.Errorf("%s (%q): Compile panicked: %v", f, c.Description, r)
					}
				}()
				res, err := schemacompiler.Compile(context.Background(), c.Schema, schemacompiler.Options{})
				if err != nil {
					errored++
					t.Errorf("%s (%q): Compile error: %v", f, c.Description, err)
					return
				}
				if res == nil {
					t.Errorf("%s (%q): Compile returned a nil result with no error", f, c.Description)
					return
				}
				dist[distKey{res.Capability, res.Exactness}]++
			}()
		}
	}

	t.Logf("walked %d suite files", len(files))
	if errored > 0 || panicked > 0 {
		t.Logf("errors=%d panics=%d", errored, panicked)
	}
	logDistribution(t, dist)
}
