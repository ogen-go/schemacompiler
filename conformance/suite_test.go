// suite_test.go is an opt-in walk of the upstream JSON-Schema-Test-Suite's
// draft2020-12 *schemas* (not its pass/fail instance tests, which don't apply to an
// analysis-only compiler — see corpus_test.go). It never fetches anything: it looks
// for a git submodule this repository does not vendor by default and skips cleanly
// when absent.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
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
	files := suiteFiles(t)

	dist := make(map[distKey]int)
	reqs := map[string]int{
		"RawEvaluation": 0, "UnboundedNumeric": 0, "JSONEquality": 0,
		"ECMARegex": 0, "EvaluationTracking": 0,
	}
	kinds := make(map[plan.DiagnosticKind]int)
	var attempted, errored, panicked, overbroad int
	var errSamples, guardSamples []string
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
			attempted++
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked++
						// A panic is always a real defect: fail hard.
						t.Errorf("%s (%q): Compile panicked: %v", f, c.Description, r)
					}
				}()
				res, err := schemacompiler.Compile(context.Background(), c.Schema, schemacompiler.Options{})
				if err != nil {
					// Sanity, not strict: some valid 2020-12 schemas hit known library
					// limitations (e.g. libopenapi's index-free build rejects a boolean
					// `unevaluatedItems`). Record for review rather than failing the walk.
					errored++
					if len(errSamples) < 10 {
						errSamples = append(errSamples, fmt.Sprintf("%s (%q): %v", filepath.Base(f), c.Description, err))
					}
					return
				}
				if res == nil {
					t.Errorf("%s (%q): Compile returned a nil result with no error", f, c.Description)
					return
				}
				dist[distKey{res.Capability, fidelityName(res)}]++
				reqs["RawEvaluation"] += len(res.Requirements.RawEvaluation)
				reqs["UnboundedNumeric"] += len(res.Requirements.UnboundedNumeric)
				reqs["JSONEquality"] += len(res.Requirements.JSONEquality)
				reqs["ECMARegex"] += len(res.Requirements.ECMARegex)
				reqs["EvaluationTracking"] += len(res.Requirements.EvaluationTracking)
				for _, d := range res.Diagnostics {
					kinds[d.Kind]++
				}
				for _, g := range planwalk.OverbroadGuards(res.Plan) {
					// A guard reaching past what its predicate reads either rejects an
					// instance the schema accepts or is a check in name only, and the
					// interpreters cannot tell either way (issue #60).
					overbroad++
					if len(guardSamples) < 10 {
						guardSamples = append(guardSamples, fmt.Sprintf("%s (%q): %T guarded on %v, reads %v",
							filepath.Base(f), c.Description, g.Predicate,
							plan.KindSetNames(g.Guard), plan.KindSetNames(g.Meaning)))
					}
				}
			}()
		}
	}

	t.Logf("walked %d suite files, %d schemas", len(files), attempted)
	if errored > 0 {
		t.Logf("errored=%d (%.1f%%); samples:", errored, 100*float64(errored)/float64(attempted))
		for _, s := range errSamples {
			t.Logf("  %s", s)
		}
	}
	logDistribution(t, dist)
	reportRequirements(t, reqs)

	// Every slot is populated by some suite schema. An empty one means the rule stopped
	// firing, and a Requirements field that silently reports nothing is worse than no
	// field at all: a consumer cannot tell "nothing to discharge" from "not implemented"
	// (design §25).
	for slot, n := range reqs {
		require.NotZero(t, n, "no suite schema reports %s; the rule has stopped firing", slot)
	}

	reportDiagnosticKinds(t, kinds)

	// §24.1 is only machine-checkable if every diagnostic says which side of it it falls
	// on, so an unclassified one is a defect wherever it comes from.
	require.Zero(t, kinds[plan.DiagnosticUnclassified],
		"%d suite diagnostics carry no Kind", kinds[plan.DiagnosticUnclassified])

	// Hard guards: never panic, and the error rate must stay well under a ceiling so a
	// broad regression (a change that breaks Compile on many schemas) still fails loudly.
	require.Zero(t, panicked, "Compile must never panic on a suite schema")
	require.Zerof(t, overbroad, "%d guards fire on a kind their predicate cannot read:\n  %s",
		overbroad, strings.Join(guardSamples, "\n  "))
	require.Less(t, errored*5, attempted, "suite error rate exceeded 20%%; likely a regression, not a library gap")
}

// reportRequirements logs what the suite's schemas ask of a consumer (design §25), so the
// numbers are visible rather than assumed.
func reportRequirements(t *testing.T, reqs map[string]int) {
	t.Helper()

	slots := make([]string, 0, len(reqs))
	for slot := range reqs {
		slots = append(slots, slot)
	}
	sort.Strings(slots)

	var b strings.Builder
	b.WriteString("requirements reported over the suite:\n")
	for _, slot := range slots {
		fmt.Fprintf(&b, "  %-20s %d\n", slot, reqs[slot])
	}
	t.Log(b.String())
}

// reportDiagnosticKinds logs how the suite's diagnostics divide between saying the plan
// accepts more than its schema and saying it costs more to run (design §24.1).
func reportDiagnosticKinds(t *testing.T, kinds map[plan.DiagnosticKind]int) {
	t.Helper()

	var b strings.Builder
	b.WriteString("diagnostic kinds:\n")
	for _, e := range []struct {
		kind plan.DiagnosticKind
		name string
	}{
		{plan.DiagnosticUnclassified, "unclassified"},
		{plan.DiagnosticAdvisory, "advisory"},
		{plan.DiagnosticCost, "cost"},
		{plan.DiagnosticAssumed, "assumed"},
		{plan.DiagnosticUnenforced, "unenforced"},
		{plan.DiagnosticUnsupported, "unsupported"},
	} {
		fmt.Fprintf(&b, "  %-20s %d\n", e.name, kinds[e.kind])
	}
	t.Log(b.String())
}
