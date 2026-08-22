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

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// outOfDialect names the suite files whose labels do not describe Draft 2020-12 as this
// compiler and this interpreter implement it, so running them would report a mismatched
// oracle as a compiler bug. Every skipped schema is still counted.
var outOfDialect = map[string]string{
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
	// interpreter itself could not enforce (a pattern its ECMA-262 engine cannot compile
	// or cannot finish, an unasserted `format`), so it is not evidence against the plan.
	verdictApproximated
	// verdictDeclaredInexact is verdictAcceptedInvalid reached through a plan that says it
	// accepts more than its schema — [plan.SoundOverApproximation] and above. The oracle
	// cannot hold a plan to a claim it does not make, so this is permitted; it is counted
	// on its own line so the price of permitting it is a number rather than an assumption.
	verdictDeclaredInexact
	// verdictInvalidValue is the interpreter refusing the instance itself: the corpus
	// decoded into something [encoding/json] does not produce. That is a harness bug.
	verdictInvalidValue
	// verdictInternalError is the interpreter unable to read the plan — a variant that
	// slipped past planterp's TestEveryVariantIsHandled, an unresolvable reference,
	// malformed plan data. It is the guard the whole design rests on, so it is never
	// quarantined and always fails.
	verdictInternalError
	// verdictRepresentationLoadBearing is the plan and its checks alone disagreeing about
	// one instance, which means some constraint is enforced only by the Go shape. Design
	// §4.1 says the checks are the whole contract, so this is a defect in the plan, not a
	// permitted approximation, and it is never quarantined.
	verdictRepresentationLoadBearing
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
	case verdictDeclaredInexact:
		return "accepted-invalid-while-declaring-inexactness"
	case verdictInvalidValue:
		return "invalid-instance-value"
	case verdictInternalError:
		return "interpreter-internal-error"
	case verdictRepresentationLoadBearing:
		return "representation-load-bearing"
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
				if permitted(kind) {
					if kind != verdictOK {
						tally.failures[kind]++
					}
					continue
				}
				if note, quarantined := diffSkips[key]; quarantined && !neverQuarantined(kind) {
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
	require.Zero(t, tally.bandFailures[verdictInternalError],
		"planterp could not read a plan in the skipped exactness=UnsupportedConversion band; "+
			"that band skips the oracle, never the interpreter's own guards")
	require.Zero(t, tally.bandFailures[verdictRepresentationLoadBearing],
		"a plan in the skipped exactness=UnsupportedConversion band enforces a constraint only "+
			"through its representation; that band skips the oracle, never design §4.1")
}

// judge applies the asymmetric oracle of issue #70 to one instance.
func judge(t *testing.T, res *schemacompiler.Result, inst diffInstance) (kind verdictKind, detail string) {
	t.Helper()

	value, err := decodeInstance(inst.Data)
	if err != nil {
		return verdictInvalidValue, "decode instance: " + err.Error()
	}
	verdict, err := planterp.Interpret(res.Plan, value)
	if err != nil {
		// Interpret never reports a rejection through its error slot, so an error here
		// is one of two unrelated defects, and they must not share a bucket: a bad
		// fixture is the harness's problem, an unreadable plan is the compiler's.
		var internal *planterp.InternalError
		if errors.As(err, &internal) {
			return verdictInternalError, err.Error()
		}
		return verdictInvalidValue, err.Error()
	}

	// Design §4.1: the validation plan and the dispatch are the whole of what a plan
	// accepts, and the representation is storage. Re-deciding the same instance without
	// the representation must reach the same verdict; if it does not, some constraint is
	// living in the Go shape and a consumer that validates from the checks alone would
	// disagree with this one (issue #115).
	checksOnly, checksErr := planterp.InterpretChecks(res.Plan, value)
	if checksErr != nil {
		return verdictInternalError, "checks-only: " + checksErr.Error()
	}
	if checksOnly.Accepted != verdict.Accepted {
		return verdictRepresentationLoadBearing, fmt.Sprintf(
			"instance %s: full plan accepted=%v, checks alone accepted=%v%s",
			inst.Data, verdict.Accepted, checksOnly.Accepted, renderReason(checksOnly.Reason))
	}

	detail = fmt.Sprintf("instance %s, capability %v, exactness %v, verdict accepted=%v%s",
		inst.Data, res.Capability, res.Exactness, verdict.Accepted, renderReason(verdict.Reason))

	switch {
	case inst.Valid && !verdict.Accepted:
		// Under-approximation: no exactness level licenses it (design §24).
		return verdictRejectedValid, detail
	case !inst.Valid && verdict.Accepted:
		// The exemption starts at SoundOverApproximation, not at DeclaredIncomplete: both
		// rungs mean the plan's accepted set is a strict superset of its schema's, so
		// neither can be held to the schema. What #95 fixed is which plans reach those
		// rungs — a representation wider than the schema is not itself inexactness when
		// the residual validator closes the gap (internal/planner/classify.go). Since that
		// fix nothing in the suite reaches them, which the tally line proves rather than
		// assumes.
		if res.Exactness >= plan.SoundOverApproximation {
			return verdictDeclaredInexact, detail
		}
		if len(verdict.Approximated) > 0 {
			return verdictApproximated, detail + ", unenforceable: " + strings.Join(verdict.Approximated, "; ")
		}
		return verdictAcceptedInvalid, detail
	default:
		return verdictOK, detail
	}
}

// renderReason indents a structured rejection under the finding it belongs to, so the
// path and the cause chain stay readable in the test log.
func renderReason(reason *planterp.ValidateError) string {
	if reason == nil {
		return ""
	}
	return "\n  " + strings.ReplaceAll(reason.Error(), "\n", "\n  ")
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

// reportedKinds is the order the tally prints its lines in.
var reportedKinds = []verdictKind{
	verdictRejectedValid,
	verdictAcceptedInvalid,
	verdictApproximated,
	verdictDeclaredInexact,
	verdictInvalidValue,
	verdictInternalError,
	verdictRepresentationLoadBearing,
}

// neverQuarantined reports whether k is a defect no quarantine entry may excuse: the
// interpreter's own guards, and design §4.1's separation of acceptance from storage.
func neverQuarantined(k verdictKind) bool {
	return k == verdictInternalError || k == verdictRepresentationLoadBearing
}

// permitted reports whether the asymmetric oracle lets k pass. The three permitted kinds
// are counted, never failed on: verdictOK is agreement, and the other two are
// disagreements the plan or the interpreter declared in advance.
func permitted(k verdictKind) bool {
	return k == verdictOK || k == verdictApproximated || k == verdictDeclaredInexact
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
	for _, kind := range reportedKinds {
		t.Logf("  %-48s %d", kind, tally.failures[kind])
	}
	t.Logf("quarantined by diffSkips: %d (entries %d)", tally.quarantined, len(diffSkips))
	t.Logf("disagreements inside the skipped exactness=UnsupportedConversion band (reported, not failures):")
	for _, kind := range reportedKinds {
		if kind == verdictApproximated || kind == verdictDeclaredInexact {
			continue
		}
		t.Logf("  %-48s %d", kind, tally.bandFailures[kind])
	}
}

// bandCost interprets the instances of a plan the oracle skips, tallying the verdicts
// without failing on them, so the price of the skip is a number in the report rather
// than an assumption in a comment.
func bandCost(t *testing.T, res *schemacompiler.Result, instances []diffInstance, into map[verdictKind]int) {
	t.Helper()

	for _, inst := range instances {
		if kind, _ := judge(t, res, inst); !permitted(kind) {
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
