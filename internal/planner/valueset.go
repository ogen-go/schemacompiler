package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
)

// valueSet deduplicates JSON literals (enum/const/discriminator values) by JSON-value
// equality ([ir.Literal.Equal]), so composite and high-precision values are handled
// correctly and never panic as Go map keys. Byte-identical values short-circuit through a
// map; the semantic scan is O(n²) in the worst case, matching ogen's own enum dedup.
type valueSet struct {
	exact map[string]struct{}
	lits  []ir.Literal
}

func newValueSet(hint int) *valueSet {
	return &valueSet{exact: make(map[string]struct{}, hint)}
}

// add records l and reports whether it was newly added (false if an equal value was
// already present).
func (s *valueSet) add(l ir.Literal) bool {
	if l.Raw != nil {
		if _, ok := s.exact[string(l.Raw)]; ok {
			return false
		}
	}
	for _, prev := range s.lits {
		if prev.Equal(l) {
			return false
		}
	}
	if l.Raw != nil {
		s.exact[string(l.Raw)] = struct{}{}
	}
	s.lits = append(s.lits, l)
	return true
}
