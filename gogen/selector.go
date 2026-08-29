package gogen

import "strconv"

// A struct's generated slots are named around the fields the schema declared, so the name
// of one depends on every field beside it. The declaration, the codec and the validators
// all have to spell the same name, which is why it is computed here and only here.
type structSlots struct {
	Patterns   []string
	Additional string
}

func slotsOf(s *Struct) structSlots {
	taken := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		taken[f.Name] = true
	}
	slots := structSlots{Patterns: make([]string, len(s.Patterns))}
	for i := range s.Patterns {
		slots.Patterns[i] = freeName("Pattern"+strconv.Itoa(i)+"Props", taken)
		taken[slots.Patterns[i]] = true
	}
	slots.Additional = freeName("AdditionalProps", taken)
	return slots
}

type tupleSlots struct {
	Elems []string
	Rest  string
}

func slotsOfTuple(t *Tuple) tupleSlots {
	taken := make(map[string]bool, len(t.Elems))
	slots := tupleSlots{Elems: make([]string, len(t.Elems))}
	for i := range t.Elems {
		slots.Elems[i] = freeName("F"+strconv.Itoa(i), taken)
		taken[slots.Elems[i]] = true
	}
	slots.Rest = freeName("Rest", taken)
	return slots
}

// selector is the Go name of the slot e fills in parent, empty when the edge reaches a
// value that is not held in a named slot of its own.
func selector(parent GoType, e Edge) string {
	switch e.Kind {
	case EdgeField:
		return e.Name
	case EdgePattern:
		if s, ok := deref(parent).(*Struct); ok {
			return slotsOf(s).Patterns[e.Index]
		}
	case EdgeAdditional:
		if s, ok := deref(parent).(*Struct); ok {
			return slotsOf(s).Additional
		}
	case EdgeTupleElem:
		if t, ok := deref(parent).(*Tuple); ok {
			return slotsOfTuple(t).Elems[e.Index]
		}
	case EdgeTupleRest:
		if t, ok := deref(parent).(*Tuple); ok {
			return slotsOfTuple(t).Rest
		}
	}
	return ""
}
