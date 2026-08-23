package nodetree

import (
	"strconv"
	"testing"
)

func stringCases(n int) []literalCase {
	out := make([]literalCase, n)
	for i := range out {
		out[i] = literalCase{raw: []byte(strconv.Quote("case-" + strconv.Itoa(i))), inner: always{}}
	}
	return out
}

func numberCases(n int) []literalCase {
	out := make([]literalCase, n)
	for i := range out {
		out[i] = literalCase{raw: []byte(strconv.Itoa(i)), inner: always{}}
	}
	return out
}

// BenchmarkSelectCase measures the crossover that sets [hashedCaseThreshold]: a scan stops
// at the first match, so it wins while the case list is short, and the table wins once the
// average scan is longer than canonicalizing the selector once. The worst-case selector is
// the last case, which is what a miss also costs.
func BenchmarkSelectCase(b *testing.B) {
	for _, shape := range []struct {
		name  string
		build func(int) []literalCase
		last  func(int) string
	}{
		{
			name:  "string",
			build: stringCases,
			last:  func(n int) string { return strconv.Quote("case-" + strconv.Itoa(n-1)) },
		},
		{
			name:  "number",
			build: numberCases,
			last:  func(n int) string { return strconv.Itoa(n - 1) },
		},
	} {
		for _, n := range []int{2, 3, 4, 6, 8, 16, 64, 256} {
			cases := shape.build(n)
			selector := []byte(shape.last(n))
			scan := caseTable{cases: cases}
			byString, byScalar, ok := indexCases(cases)
			if !ok {
				b.Fatal("cases must be keyable")
			}
			table := caseTable{cases: cases, byString: byString, byScalar: byScalar, hashed: true}

			b.Run(shape.name+"/"+strconv.Itoa(n)+"/scan", func(b *testing.B) {
				for b.Loop() {
					if _, ok := scan.selectCase(selector); !ok {
						b.Fatal("no match")
					}
				}
			})
			b.Run(shape.name+"/"+strconv.Itoa(n)+"/table", func(b *testing.B) {
				for b.Loop() {
					if _, ok := table.selectCase(selector); !ok {
						b.Fatal("no match")
					}
				}
			})
		}
	}
}
