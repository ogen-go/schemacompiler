// corpus_test.go walks the curated schemas under testdata/corpus and checks that
// Compile behaves soundly on every representative construct (design §24-25). See
// package doc in manifest_test.go for the rationale behind a coverage harness
// instead of a pass/fail instance-test runner.
package conformance

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

//go:embed testdata/corpus
var corpusFS embed.FS

const corpusRoot = "testdata/corpus"

// capabilityName renders a plan.CapabilityLevel for the distribution table; the
// plan package intentionally has no Stringer (it is pure data), so the harness
// owns this presentation concern.
func capabilityName(c plan.CapabilityLevel) string {
	switch c {
	case plan.DirectGoType:
		return "DirectGoType"
	case plan.GoTypeWithValidation:
		return "GoTypeWithValidation"
	case plan.StaticDispatch:
		return "StaticDispatch"
	case plan.PredicateDispatch:
		return "PredicateDispatch"
	case plan.EvaluationStateValidation:
		return "EvaluationStateValidation"
	case plan.DynamicSchemaResolution:
		return "DynamicSchemaResolution"
	case plan.Unsupported:
		return "Unsupported"
	default:
		return fmt.Sprintf("Capability(%d)", c)
	}
}

func exactnessName(e plan.Exactness) string {
	switch e {
	case plan.ExactPureRepresentation:
		return "ExactPureRepresentation"
	case plan.ExactWithValidation:
		return "ExactWithValidation"
	case plan.SoundOverApproximation:
		return "SoundOverApproximation"
	case plan.UnsupportedConversion:
		return "UnsupportedConversion"
	default:
		return fmt.Sprintf("Exactness(%d)", e)
	}
}

func severityName(s plan.Severity) string {
	switch s {
	case plan.SeverityInfo:
		return "Info"
	case plan.SeverityWarning:
		return "Warning"
	case plan.SeverityError:
		return "Error"
	default:
		return fmt.Sprintf("Severity(%d)", s)
	}
}

// distKey buckets one (capability, exactness) pair for the corpus distribution table.
type distKey struct {
	capability plan.CapabilityLevel
	exactness  plan.Exactness
}

// compileSafely runs Compile with a panic guard, so one malformed corpus schema
// reports as a single test failure instead of crashing the whole run (the harness
// itself asserts Compile never panics — this is what enforces that).
func compileSafely(t *testing.T, data []byte) (res *schemacompiler.Result, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Compile panicked: %v", r)
		}
	}()
	return schemacompiler.Compile(context.Background(), data, schemacompiler.Options{})
}

// TestCorpus walks every schema in testdata/corpus, asserts Compile succeeds without
// error or panic and always returns a plan, and checks capability/exactness against
// the manifest where recorded. It also aggregates a capability/exactness distribution
// and logs every diagnostic at SeverityWarning or above, so unsupported constructs are
// reported explicitly rather than silently capped (design §25, "no silent caps").
func TestCorpus(t *testing.T) {
	var paths []string
	require.NoError(t, fs.WalkDir(corpusFS, corpusRoot, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		paths = append(paths, p)
		return nil
	}))
	require.NotEmpty(t, paths, "corpus must not be empty")

	dist := make(map[distKey]int)
	var flagged []string // schemas with a Warning/Error diagnostic

	seen := make(map[string]bool, len(manifest))
	for _, p := range paths {
		rel := strings.TrimPrefix(p, corpusRoot+"/")
		seen[rel] = true

		t.Run(rel, func(t *testing.T) {
			data, err := corpusFS.ReadFile(p)
			require.NoError(t, err, "read corpus file")

			res, err := compileSafely(t, data)
			require.NoError(t, err, "Compile must not error on a well-formed corpus schema")
			require.NotNil(t, res, "Compile must return a non-nil result")
			require.NotNil(t, res.Plan.Representation, "plan must carry a representation")

			dist[distKey{res.Capability, res.Exactness}]++

			for _, diag := range res.Diagnostics {
				if diag.Severity >= plan.SeverityWarning {
					flagged = append(flagged, fmt.Sprintf("%s: [%s] %s (pointer=%q)",
						rel, severityName(diag.Severity), diag.Message, diag.Pointer))
				}
			}

			want, ok := manifest[rel]
			if !ok {
				t.Logf("no manifest expectation recorded for %s (capability=%s, exactness=%s)",
					rel, capabilityName(res.Capability), exactnessName(res.Exactness))
				return
			}
			require.Equal(t, want.Capability, res.Capability, "capability")
			if want.checkExact {
				require.Equal(t, want.Exactness, res.Exactness, "exactness")
			}
			if want.wantDiagnostic {
				require.NotEmpty(t, res.Diagnostics, "expected at least one diagnostic")
			}
		})
	}

	// Every manifest entry should correspond to a real corpus file, so the manifest
	// cannot silently drift from the corpus on disk.
	for rel := range manifest {
		require.True(t, seen[rel], "manifest entry %s has no corresponding corpus file", rel)
	}

	logDistribution(t, dist)
	logFlagged(t, flagged)
}

func logDistribution(t *testing.T, dist map[distKey]int) {
	t.Helper()
	type row struct {
		capability plan.CapabilityLevel
		exactness  plan.Exactness
		count      int
	}
	var rows []row
	for k, v := range dist {
		rows = append(rows, row{k.capability, k.exactness, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].capability != rows[j].capability {
			return rows[i].capability < rows[j].capability
		}
		return rows[i].exactness < rows[j].exactness
	})

	var b strings.Builder
	b.WriteString("capability/exactness distribution over the curated corpus:\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-28s %-26s %d\n", capabilityName(r.capability), exactnessName(r.exactness), r.count)
	}
	t.Log(b.String())
}

func logFlagged(t *testing.T, flagged []string) {
	t.Helper()
	sort.Strings(flagged)
	var b strings.Builder
	fmt.Fprintf(&b, "schemas with a Warning/Error diagnostic (%d) — no silent caps:\n", len(flagged))
	for _, f := range flagged {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	t.Log(b.String())
}
