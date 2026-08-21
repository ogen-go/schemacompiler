// differential_test.go runs the JSON-Schema-Test-Suite's labeled instances through
// internal/planterp, which decides accept/reject by reading the compilation plan and
// nothing else (issue #70). It is the machine check on Exactness: a plan that claims to
// be exact and still accepts an instance the suite calls invalid has lost a constraint.
package conformance

import (
	"bytes"
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
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// outOfDialect names the suite files whose labels do not describe Draft 2020-12 as this
// compiler and this interpreter implement it, so running them would report a mismatched
// oracle as a compiler bug. Every skipped schema is still counted.
var outOfDialect = map[string]string{
	// These schemas assert ECMA-262 regex corners (\s over Unicode whitespace, the BOM)
	// that Go's RE2 does not share. That is the interpreter's engine, not the plan.
	"optional/ecmascript-regex.json": "RE2 is not ECMA-262",
	// `dependencies` is a draft-04/07 keyword. Draft 2020-12 split it into
	// dependentRequired/dependentSchemas and treats the old spelling as unknown, so
	// ignoring it is correct behavior, not a dropped constraint.
	"optional/dependencies-compatibility.json": "draft-04 `dependencies` is not a 2020-12 keyword",
}

// diffCase mirrors one entry of a suite file, instances included.
type diffCase struct {
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Tests       []diffInstance  `json:"tests"`
}

// diffInstance is one labeled instance of a [diffCase].
type diffInstance struct {
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data"`
	Valid       bool            `json:"valid"`
}

// verdictKind classifies one interpreted instance.
type verdictKind int

const (
	// verdictOK is a verdict the asymmetric oracle permits.
	verdictOK verdictKind = iota
	// verdictRejectedValid is the plan rejecting an instance the suite calls valid: an
	// under-approximation, which design §24 forbids at every exactness level.
	verdictRejectedValid
	// verdictAcceptedInvalid is the plan accepting an instance the suite calls invalid
	// while claiming ExactPureRepresentation or ExactWithValidation.
	verdictAcceptedInvalid
	// verdictApproximated is verdictAcceptedInvalid reached through a constraint the
	// interpreter itself could not enforce (an RE2-incompatible pattern, an unasserted
	// `format`), so it is not evidence against the plan.
	verdictApproximated
	// verdictInterpError is the interpreter refusing to give a verdict at all.
	verdictInterpError
)

func (k verdictKind) String() string {
	switch k {
	case verdictOK:
		return "ok"
	case verdictRejectedValid:
		return "rejected-valid"
	case verdictAcceptedInvalid:
		return "accepted-invalid-while-exact"
	case verdictApproximated:
		return "accepted-invalid-via-unenforceable-constraint"
	case verdictInterpError:
		return "interpreter-error"
	default:
		return fmt.Sprintf("verdictKind(%d)", int(k))
	}
}

// diffTally counts everything the sweep saw, so the corpus cannot silently shrink.
type diffTally struct {
	files          int
	schemas        int
	instances      int
	skippedCompile int
	// unsupportedCapability and unsupportedExactness count the plans skipped because no
	// backend would generate them at all (docs/integration.md §6).
	unsupportedCapability int
	unsupportedExactness  int
	// outOfDialect counts the schemas skipped per [outOfDialect] reason.
	outOfDialect map[string]int
	failures     map[verdictKind]int
	// bandFailures counts the disagreements inside the skipped
	// Exactness == UnsupportedConversion band. They are reported, never failed on: the
	// line exists so the cost of skipping the band cannot silently grow.
	bandFailures map[verdictKind]int
	quarantined  int
	unusedSkips  []string
}

// TestPlanInterpreterDifferential compiles every draft2020-12 suite schema, interprets
// every labeled instance against the resulting plan, and applies the asymmetric oracle
// of issue #70.
//
//	git submodule update --init testdata/JSON-Schema-Test-Suite
//	go test ./conformance/... -run TestPlanInterpreterDifferential -v
func TestPlanInterpreterDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JSON-Schema-Test-Suite differential run in -short mode")
	}
	files := suiteFiles(t)

	tally := diffTally{
		files:        len(files),
		outOfDialect: map[string]int{},
		failures:     map[verdictKind]int{},
		bandFailures: map[verdictKind]int{},
	}
	seen := map[string]bool{}
	var findings []string

	for _, file := range files {
		rel, err := filepath.Rel(suiteRoot, file)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)

		data, err := os.ReadFile(file) //nolint:gosec // test-only, path from a controlled walk
		require.NoError(t, err)
		var cases []diffCase
		if err := json.Unmarshal(data, &cases); err != nil {
			continue // not a {description, schema, tests} file.
		}
		if reason, ok := outOfDialect[rel]; ok {
			tally.outOfDialect[reason] += len(cases)
			tally.schemas += len(cases)
			continue
		}

		for _, c := range cases {
			if len(c.Schema) == 0 {
				continue
			}
			tally.schemas++

			res, err := compileQuietly(c.Schema)
			if err != nil {
				tally.skippedCompile++
				continue
			}
			switch {
			case res.Capability == plan.Unsupported:
				tally.unsupportedCapability++
				continue
			case res.Exactness == plan.UnsupportedConversion:
				// These plans convert to nothing at all, so no backend reaches an
				// instance through them (docs/integration.md §6) and the oracle has
				// nothing to hold them to. The band is still interpreted, and what it
				// costs is reported on its own tally line rather than assumed: at the
				// time of writing it hides 4 rejected-valid instances, all in
				// unevaluatedProperties.json, all attributable to #5 (unevaluated* is
				// unimplemented, which is why these plans are UnsupportedConversion in
				// the first place), and no accepted-invalid-while-exact at all. Keeping
				// the line means that cost tracks the compiler instead of going stale.
				tally.unsupportedExactness++
				bandCost(t, res, c.Tests, tally.bandFailures)
				continue
			}

			for _, inst := range c.Tests {
				tally.instances++
				key := rel + " :: " + c.Description + " :: " + inst.Description
				kind, detail := judge(t, res, inst)
				if kind == verdictOK || kind == verdictApproximated {
					if kind == verdictApproximated {
						tally.failures[verdictApproximated]++
					}
					continue
				}
				if note, quarantined := diffSkips[key]; quarantined {
					seen[key] = true
					tally.quarantined++
					tally.failures[kind]++
					t.Logf("quarantined (%s): %s\n  %s\n  %s", note, key, kind, detail)
					continue
				}
				tally.failures[kind]++
				findings = append(findings, key+"\n  "+kind.String()+"\n  "+detail)
			}
		}
	}

	for key := range diffSkips {
		if !seen[key] {
			tally.unusedSkips = append(tally.unusedSkips, key)
		}
	}
	sort.Strings(tally.unusedSkips)

	reportTally(t, tally)
	for _, f := range findings {
		t.Errorf("plan/suite disagreement: %s", f)
	}
	require.Empty(t, tally.unusedSkips,
		"quarantine entries that no longer fire: the bug is fixed, delete the entry")
}

// judge applies the asymmetric oracle of issue #70 to one instance.
func judge(t *testing.T, res *schemacompiler.Result, inst diffInstance) (kind verdictKind, detail string) {
	t.Helper()

	value, err := decodeInstance(inst.Data)
	if err != nil {
		return verdictInterpError, "decode instance: " + err.Error()
	}
	verdict, err := planterp.Interpret(res.Plan, value)
	if err != nil {
		return verdictInterpError, err.Error()
	}

	detail = fmt.Sprintf("instance %s, capability %v, exactness %v, verdict accepted=%v %s",
		inst.Data, res.Capability, res.Exactness, verdict.Accepted, verdict.Reason)

	switch {
	case inst.Valid && !verdict.Accepted:
		// Under-approximation: no exactness level licenses it (design §24).
		return verdictRejectedValid, detail
	case !inst.Valid && verdict.Accepted:
		if res.Exactness >= plan.SoundOverApproximation {
			return verdictOK, detail
		}
		if len(verdict.Approximated) > 0 {
			return verdictApproximated, detail + ", unenforceable: " + strings.Join(verdict.Approximated, "; ")
		}
		return verdictAcceptedInvalid, detail
	default:
		return verdictOK, detail
	}
}

// decodeInstance decodes with UseNumber so that a literal past float64's precision, and
// `multipleOf` on decimals, compare exactly rather than through a rounded float.
func decodeInstance(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// compileQuietly compiles one suite schema, turning a panic into an error so a single
// bad schema cannot abort the sweep. A panic is still a defect and is reported by
// TestJSONSchemaTestSuite, which exists to catch exactly that.
func compileQuietly(schema json.RawMessage) (res *schemacompiler.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("compile panicked: %v", r)
		}
	}()
	return schemacompiler.Compile(context.Background(), schema, schemacompiler.Options{})
}

func suiteFiles(t *testing.T) []string {
	t.Helper()

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
		if !fi.IsDir() && strings.HasSuffix(p, ".json") {
			files = append(files, p)
		}
		return nil
	})
	require.NoError(t, walkErr)
	if len(files) == 0 {
		t.Skip("JSON-Schema-Test-Suite submodule present but empty; nothing to walk")
	}
	sort.Strings(files)
	return files
}

func reportTally(t *testing.T, tally diffTally) {
	t.Helper()

	t.Logf("suite files %d, schemas %d (compiled %d), instances checked %d",
		tally.files, tally.schemas,
		tally.schemas-tally.skippedCompile-tally.unsupportedCapability-tally.unsupportedExactness-outOfDialectTotal(tally),
		tally.instances)
	t.Logf("skipped: compile-error %d, capability=Unsupported %d, exactness=UnsupportedConversion %d",
		tally.skippedCompile, tally.unsupportedCapability, tally.unsupportedExactness)
	for _, reason := range sortedKeys(tally.outOfDialect) {
		t.Logf("skipped: out of dialect (%s) %d", reason, tally.outOfDialect[reason])
	}
	for _, kind := range []verdictKind{verdictRejectedValid, verdictAcceptedInvalid, verdictApproximated, verdictInterpError} {
		t.Logf("  %-48s %d", kind, tally.failures[kind])
	}
	t.Logf("quarantined by diffSkips: %d (entries %d)", tally.quarantined, len(diffSkips))
	t.Logf("disagreements inside the skipped exactness=UnsupportedConversion band (reported, not failures):")
	for _, kind := range []verdictKind{verdictRejectedValid, verdictAcceptedInvalid, verdictInterpError} {
		t.Logf("  %-48s %d", kind, tally.bandFailures[kind])
	}
}

// bandCost interprets the instances of a plan the oracle skips, tallying the verdicts
// without failing on them, so the price of the skip is a number in the report rather
// than an assumption in a comment.
func bandCost(t *testing.T, res *schemacompiler.Result, instances []diffInstance, into map[verdictKind]int) {
	t.Helper()

	for _, inst := range instances {
		if kind, _ := judge(t, res, inst); kind != verdictOK && kind != verdictApproximated {
			into[kind]++
		}
	}
}

func outOfDialectTotal(tally diffTally) int {
	total := 0
	for _, n := range tally.outOfDialect {
		total += n
	}
	return total
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
