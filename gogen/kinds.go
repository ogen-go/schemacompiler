package gogen

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// Kinds is the set of JSON kinds a value of type t can hold.
//
// It is what makes a kind guard decidable against a Go type: a predicate guarded on
// [plan.SetString] is dead weight on a type that cannot hold a string, and an assertion is
// already made by a type that can hold nothing else.
func Kinds(t GoType) plan.KindSet {
	return kinds(t, make(map[GoType]bool))
}

func kinds(t GoType, seen map[GoType]bool) plan.KindSet {
	if seen[t] {
		// A recursive type is reached through its own definition; the kinds it holds are
		// the ones the outer visit is computing, so contributing nothing is the fixpoint.
		return 0
	}
	seen[t] = true

	switch t := t.(type) {
	case *Named:
		return kinds(t.Underlying, seen)
	case *Pointer:
		return kinds(t.Elem, seen)
	case *Presence:
		k := kinds(t.Elem, seen)
		if t.Nullable {
			k |= plan.SetNull
		}
		return k
	case *Interface:
		var k plan.KindSet
		for _, v := range t.Variants {
			k |= kinds(v, seen)
		}
		return k
	case *Struct, *Map:
		return plan.SetObject
	case *Slice, *Tuple:
		return plan.SetArray
	case *Primitive:
		return primitiveKinds(t.Kind)
	case *Any:
		return plan.SetAny
	case *Never:
		return 0
	default:
		panic(fmt.Sprintf("gogen: unhandled GoType variant %T", t))
	}
}

func primitiveKinds(k PrimitiveKind) plan.KindSet {
	switch k {
	case PrimitiveString:
		return plan.SetString
	case PrimitiveBool:
		return plan.SetBoolean
	case PrimitiveInt, PrimitiveFloat:
		return plan.SetNumber
	case PrimitiveNull:
		return plan.SetNull
	default:
		panic(fmt.Sprintf("gogen: unhandled PrimitiveKind %v", k))
	}
}

// storesAs reports whether every part of t that can hold a want-kinded value satisfies ok.
//
// A part that cannot hold one is not a counterexample: it is unreachable under the guard
// the predicate carries, so nothing it does can make the check wrong.
func storesAs(t GoType, want plan.KindSet, ok func(GoType) bool) bool {
	seen := make(map[GoType]bool)
	var walk func(GoType) bool
	walk = func(t GoType) bool {
		if seen[t] {
			return true
		}
		seen[t] = true
		switch t := t.(type) {
		case *Named:
			return walk(t.Underlying)
		case *Pointer:
			return walk(t.Elem)
		case *Presence:
			return walk(t.Elem)
		case *Interface:
			for _, v := range t.Variants {
				if Kinds(v)&want != 0 && !walk(v) {
					return false
				}
			}
			return true
		default:
			if Kinds(t)&want == 0 {
				return true
			}
			return ok(t)
		}
	}
	return walk(t)
}

// exact reports whether t stores its values structurally all the way down, with no [Any]
// holding raw JSON. It is what a check comparing whole values needs, since two raw
// documents can differ byte for byte and be the same JSON value.
func exact(t GoType) bool {
	seen := make(map[GoType]bool)
	var walk func(GoType) bool
	walk = func(t GoType) bool {
		if seen[t] {
			return true
		}
		seen[t] = true
		switch t := t.(type) {
		case *Any:
			return false
		case *Named:
			return walk(t.Underlying)
		case *Pointer:
			return walk(t.Elem)
		case *Presence:
			return walk(t.Elem)
		case *Slice:
			return walk(t.Elem)
		case *Map:
			return walk(t.Elem)
		case *Interface:
			for _, v := range t.Variants {
				if !walk(v) {
					return false
				}
			}
			return true
		case *Struct:
			for _, f := range t.Fields {
				if !walk(f.Type) {
					return false
				}
			}
			for _, p := range t.Patterns {
				if !walk(p) {
					return false
				}
			}
			return t.Additional == nil || walk(t.Additional)
		case *Tuple:
			for _, e := range t.Elems {
				if !walk(e) {
					return false
				}
			}
			return t.Rest == nil || walk(t.Rest)
		case *Primitive, *Never:
			return true
		default:
			panic(fmt.Sprintf("gogen: unhandled GoType variant %T", t))
		}
	}
	return walk(t)
}
