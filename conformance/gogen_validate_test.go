package conformance

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gotypecheck"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestGogenRendersValidatorsForCorpus measures what generated validation covers, and what
// it admits to leaving undone. A constraint no check is written for is reported rather than
// dropped: over-accepting is what design §24 permits, and only while it is declared.
func TestGogenRendersValidatorsForCorpus(t *testing.T) {
	reasons := map[string]int{}
	var docs, lowered, validated int
	eachOgenDocument(t, func(rel string, defs map[plan.SchemaID]plan.CompilationPlan) {
		types, err := gogen.Lower(defs)
		if err != nil {
			return
		}
		files, err := gogen.Render(types, gogen.Options{Codec: true, Validate: true})
		require.NoError(t, err, rel)
		require.NoError(t, gotypecheck.Check(files, "../opt"), "%s does not type-check", rel)
		docs++
		lowered += len(types)
		validated += strings.Count(string(files[0].Content), ") Validate() error {")
		for reason, n := range gogen.Unenforced(types) {
			reasons[reason] += n
		}
	})

	require.NotZero(t, docs)
	total := 0
	for _, n := range reasons {
		total += n
	}
	t.Logf("%d documents, %d types, %d validators, %d unenforced", docs, lowered, validated, total)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return reasons[b] - reasons[a] })
	for _, k := range keys {
		t.Logf("  %5d %s", reasons[k], k)
	}
}
