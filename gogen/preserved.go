package gogen

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// Disposition is what a backend must do about one predicate, given the Go type it chose.
type Disposition uint8

const (
	// Discharged means the Go type already enforces it and nothing need be emitted: a
	// kind assertion over a type that holds only that kind, a `required` property stored
	// in a field that is not optional, a structural check restating the very shape the
	// type was lowered from.
	Discharged Disposition = iota
	// Inline means generated code can decide it from the decoded value.
	Inline
	// Delegate means it cannot, and the check must run against the raw JSON.
	Delegate
)

var dispositionNames = [...]string{Discharged: "discharged", Inline: "inline", Delegate: "delegate"}

func (d Disposition) String() string {
	if int(d) >= len(dispositionNames) {
		return "disposition(?)"
	}
	return dispositionNames[d]
}

// Classify decides what t must do about gp.
//
// The rule is docs/backend.md §2's: a predicate is inlined iff the Go type preserves
// everything that predicate reads. That is a property of the *pair*, and it is why
// [plan.CompilationPlan.ResidualChecks] cannot answer it — that discharges against the
// plan's own representation, which is not the shape `Lower` chose.
func Classify(t GoType, gp plan.GuardedPredicate) Disposition {
	switch {
	case vacuous(t, gp):
		return Discharged
	case dischargedBy(t, gp):
		return Discharged
	case preservedBy(t, gp.Expression):
		return Inline
	default:
		return Delegate
	}
}

// vacuous reports whether gp can never have anything to say about a value of type t.
//
// A guard the type never enters is dead. An assertion is different: it rejects a value
// whose kind is outside the guard, so it is spent only when the type cannot hold one.
func vacuous(t GoType, gp plan.GuardedPredicate) bool {
	k := Kinds(t)
	if gp.Assert {
		return k&^gp.Applicability == 0
	}
	return k&gp.Applicability == 0
}

// dischargedBy reports whether the Go type states gp itself, so that emitting the check
// would restate what the type already says.
func dischargedBy(t GoType, gp plan.GuardedPredicate) bool {
	switch e := gp.Expression.(type) {
	case nil:
		// A bare kind guard carries no expression; vacuous has already spent the cases
		// where the type settles it.
		return false
	case *plan.NumericDomainPredicate:
		return storesAs(t, plan.SetNumber, func(x GoType) bool {
			p, ok := x.(*Primitive)
			return ok && p.Kind == PrimitiveInt && e.Domain == plan.IntegerOnly
		})
	case *plan.ReferencePredicate:
		return namesTarget(t, e.Name)
	case *plan.RequiredPredicate:
		return requiredByType(t, e.Properties)
	default:
		return false
	}
}

// requiredByType reports whether every listed property is stored in a slot that cannot be
// absent. A field the lowering did not wrap in [Presence] is exactly that claim: the
// decoded value has nowhere to record that the property was missing, so a decoder that
// produced one has already accepted that it was there.
func requiredByType(t GoType, names []string) bool {
	s, ok := deref(t).(*Struct)
	if !ok {
		return false
	}
	for _, name := range names {
		i := fieldIndex(s, name)
		if i < 0 {
			return false
		}
		if p, opt := s.Fields[i].Type.(*Presence); opt && p.Optional {
			return false
		}
	}
	return true
}

func fieldIndex(s *Struct, jsonName string) int {
	for i, f := range s.Fields {
		if f.JSON == jsonName {
			return i
		}
	}
	return -1
}

// preservedBy reports whether the decoded value of type t retains everything e reads.
//
// The four variants that never survive are [plan.CapabilityLevel]'s [plan.RawEvaluation]
// members (design §4.2). That is not a coincidence and not a shortcut: the ladder ranks how
// much raw JSON generated code must retain and inspect, so "needs the document" and "no Go
// type preserves it" are the same statement read from two ends.
func preservedBy(t GoType, e plan.PredicateExpr) bool {
	switch e := e.(type) {
	case nil:
		// A bare guard reads only the instance's kind. A typed value carries that in its
		// Go type; raw storage carries it in the first byte of a document, which is the
		// document this is trying not to consult.
		return typedKind(t)

	case *plan.NegationPredicate, *plan.ShapePredicate,
		*plan.ContainsCountPredicate, *plan.PropertyNamesPredicate:
		return false

	case *plan.MinLengthPredicate, *plan.MaxLengthPredicate,
		*plan.PatternPredicate, *plan.FormatPredicate:
		return storesAs(t, plan.SetString, isPrimitive(PrimitiveString))

	case *plan.MinimumPredicate, *plan.MaximumPredicate,
		*plan.MultipleOfPredicate, *plan.NumericDomainPredicate:
		return storesAs(t, plan.SetNumber, isNumber)

	case *plan.MinItemsPredicate, *plan.MaxItemsPredicate:
		return storesAs(t, plan.SetArray, isArray)

	case *plan.UniqueItemsPredicate:
		// Uniqueness compares whole elements, and two raw documents that differ byte for
		// byte can be the same JSON value, so nothing under the array may be raw.
		return storesAs(t, plan.SetArray, func(x GoType) bool { return isArray(x) && exact(x) })

	case *plan.MinPropertiesPredicate, *plan.MaxPropertiesPredicate:
		// Counting keys needs every key to have landed somewhere. It always has:
		// `Lower` states `additionalProperties` in every object, so an open one has an
		// overflow map and a closed one admits no key it did not store.
		return storesAs(t, plan.SetObject, isObject)

	case *plan.RequiredPredicate, *plan.DependentRequiredPredicate:
		return storesAs(t, plan.SetObject, isObject)

	case *plan.ObjectStructurePredicate:
		return storesAs(t, plan.SetObject, isObject)

	case *plan.ArrayStructurePredicate:
		return storesAs(t, plan.SetArray, isArray)

	case *plan.ReferencePredicate:
		return true

	default:
		// Every variant is named above. A new one must be classified deliberately, and
		// guessing either way is wrong: guessing Inline emits a check that cannot be
		// written, guessing Delegate hides a keyword nothing enforces.
		panic(fmt.Sprintf("gogen: unhandled plan.PredicateExpr variant %T", e))
	}
}

// typedKind reports whether the Go type settles the JSON kind of the value it holds.
func typedKind(t GoType) bool {
	return storesAs(t, plan.SetAny, func(x GoType) bool {
		_, raw := x.(*Any)
		return !raw
	})
}

func isPrimitive(k PrimitiveKind) func(GoType) bool {
	return func(x GoType) bool {
		p, ok := x.(*Primitive)
		return ok && p.Kind == k
	}
}

func isNumber(x GoType) bool {
	p, ok := x.(*Primitive)
	return ok && (p.Kind == PrimitiveInt || p.Kind == PrimitiveFloat)
}

func isArray(x GoType) bool {
	switch x.(type) {
	case *Slice, *Tuple:
		return true
	default:
		return false
	}
}

func isObject(x GoType) bool {
	switch x.(type) {
	case *Struct, *Map:
		return true
	default:
		return false
	}
}

// deref looks through the wrappers that do not change what is stored, so a check can ask
// about the shape without caring how it is held. A [Named] is one of them: a declared type
// stores exactly what it is declared as.
func deref(t GoType) GoType {
	seen := make(map[GoType]bool)
	for !seen[t] {
		seen[t] = true
		switch x := t.(type) {
		case *Pointer:
			t = x.Elem
		case *Presence:
			t = x.Elem
		case *Named:
			t = x.Underlying
		default:
			return t
		}
	}
	return t
}

// namesTarget reports whether name is one of the declared types t resolves through. A plan
// whose representation is a reference lowers to that reference's [Named], so the predicate
// restating the reference is already what the type says.
func namesTarget(t GoType, name string) bool {
	seen := make(map[GoType]bool)
	for !seen[t] {
		seen[t] = true
		switch x := t.(type) {
		case *Named:
			if string(x.ID) == name {
				return true
			}
			t = x.Underlying
		case *Pointer:
			t = x.Elem
		case *Presence:
			t = x.Elem
		default:
			return false
		}
	}
	return false
}
