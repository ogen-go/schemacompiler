package nodetree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCaseTableAgreesWithTheScan pins the property the lookup table rests on: a map can
// only stand in for [jsonequal.Equal] if equal values key alike. The spellings below are
// the ones that break a table keyed on the source bytes — `1`, `1.0` and `1e0` are one
// value, and so are `"a"` and its escaped form.
func TestCaseTableAgreesWithTheScan(t *testing.T) {
	literals := []string{
		`null`, `true`, `false`, `"a"`, `"a/b"`, `""`, `0`, `1`, `1.5`, `-2`,
		`100000000000000000000000000001`,
	}

	cases := make([]literalCase, len(literals))
	for i, l := range literals {
		cases[i] = literalCase{raw: []byte(l), inner: allocated(i)}
	}
	byString, byScalar, ok := indexCases(cases)
	require.True(t, ok, "every literal here is a scalar with a distinct value")

	scan := caseTable{cases: cases}
	table := caseTable{cases: cases, byString: byString, byScalar: byScalar, hashed: true}

	for _, selector := range append([]string{
		`1.0`, `1e0`, `1.0e0`, `0.0`, `-0`, `15e-1`, `1.50`, `-2.0`,
		`"a"`, `"a\/b"`,
		`100000000000000000000000000001.0`, `1.00000000000000000000000000001e29`,
		`2`, `"z"`, `{}`, `[]`, `{"a":1}`, `not json`,
	}, literals...) {
		t.Run(selector, func(t *testing.T) {
			want, wantOK := scan.selectCase([]byte(selector))
			got, gotOK := table.selectCase([]byte(selector))
			require.Equal(t, wantOK, gotOK)
			require.Equal(t, want, got)
		})
	}
}

// allocated is a node distinguishable by identity, so a test can tell which case a
// selector chose rather than only that it chose one.
type allocated int

func (allocated) ok([]byte) bool { return true }
