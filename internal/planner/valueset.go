package planner

import "reflect"

// valueSet deduplicates JSON values while tolerating composite ones. Arrays
// ([]any) and objects (map[string]any) are valid enum/const values but are not
// valid Go map keys, so hashing them would panic. Scalars use a fast map; composites
// fall back to a linear reflect.DeepEqual scan (branch counts are small).
type valueSet struct {
	scalars   map[any]struct{}
	composite []any
}

func newValueSet(hint int) *valueSet {
	return &valueSet{scalars: make(map[any]struct{}, hint)}
}

// add records v and reports whether it was newly added (false if an equal value was
// already present).
func (s *valueSet) add(v any) bool {
	if hashableJSON(v) {
		if _, ok := s.scalars[v]; ok {
			return false
		}
		s.scalars[v] = struct{}{}
		return true
	}
	for _, prev := range s.composite {
		if reflect.DeepEqual(prev, v) {
			return false
		}
	}
	s.composite = append(s.composite, v)
	return true
}

// hashableJSON reports whether v (a json.Unmarshal value) is safe to use as a Go map
// key: everything except the composite JSON kinds.
func hashableJSON(v any) bool {
	switch v.(type) {
	case []any, map[string]any:
		return false
	default:
		return true
	}
}
