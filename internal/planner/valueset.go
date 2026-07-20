package planner

import (
	"bytes"
	"reflect"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/jsonequal"
)

// valueSet deduplicates JSON literals (enum/const/discriminator values) by semantic
// equality. It compares the preserved raw bytes with [jsonequal.Equal] (exact numeric
// comparison, order-independent objects), so composite and high-precision values are
// handled correctly and never panic as Go map keys. Byte-identical values short-circuit
// through a map; the semantic scan is O(n²) in the worst case, matching ogen's own enum
// dedup. Literals without raw bytes fall back to reflect.DeepEqual on the decoded value.
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
		if literalEqual(prev, l) {
			return false
		}
	}
	if l.Raw != nil {
		s.exact[string(l.Raw)] = struct{}{}
	}
	s.lits = append(s.lits, l)
	return true
}

func literalEqual(a, b ir.Literal) bool {
	if a.Raw != nil && b.Raw != nil {
		if bytes.Equal(a.Raw, b.Raw) {
			return true
		}
		eq, err := jsonequal.Equal(a.Raw, b.Raw)
		return err == nil && eq
	}
	return reflect.DeepEqual(a.Value, b.Value)
}
