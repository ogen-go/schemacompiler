package gogen

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// DispatchKind names which [plan.DispatchPlan] variant a plan selects branches with.
//
// It is kept beside the disposition because a backend that must admit a dispatch went
// unlowered has to say what it was, and by then it holds a [Named] rather than a plan.
type DispatchKind uint8

const (
	// DispatchNone is a plan with a single representation and nothing to select.
	DispatchNone DispatchKind = iota
	DispatchLiteral
	DispatchJSONKind
	DispatchProperty
	DispatchPresence
	DispatchPredicateCount
)

var dispatchKindNames = [...]string{
	DispatchNone:           "no",
	DispatchLiteral:        "literal",
	DispatchJSONKind:       "kind",
	DispatchProperty:       "property",
	DispatchPresence:       "presence",
	DispatchPredicateCount: "predicate-count",
}

func (k DispatchKind) String() string {
	if int(k) >= len(dispatchKindNames) {
		return "dispatch(?)"
	}
	return dispatchKindNames[k]
}

// DispatchCheck is what a backend must do about a plan's branch selection, and which
// selection it was.
type DispatchCheck struct {
	Kind        DispatchKind
	Disposition Disposition
}

// ClassifyDispatch decides what t must do about d.
//
// Dispatch is a fourth concern of a [plan.CompilationPlan] beside representation,
// validation and resolution (design §9), so it is classified separately from
// [Classify] — but into the same three answers, because a backend acts on it the same
// way. Discharged means the chosen Go type already states the selection: `Lower` folds a
// [plan.LiteralDispatch] into an [Enum] and the codec enforces it. Everything else is
// outstanding, and saying so is the point (issue #155): the shape is the same whichever
// branch runs, so a `Checks` that stayed silent would let a backend read no delegated
// predicate and conclude nothing was left undone.
//
// The split between Inline and Delegate is design §4.2's capability ladder, the same
// reading [preservedBy] takes. A kind, a tag property and a property's presence are all
// decidable from the decoded value; [plan.PredicateCountDispatch] trial-validates every
// branch, which is what [plan.RawEvaluation] names, so it can only ever run against the
// document.
func ClassifyDispatch(t GoType, d plan.DispatchPlan) DispatchCheck {
	switch d := d.(type) {
	case nil:
		return DispatchCheck{}
	case *plan.NoDispatch:
		return DispatchCheck{}
	case *plan.LiteralDispatch:
		if len(d.Cases) == 0 || foldedToEnum(t) {
			return DispatchCheck{Kind: DispatchLiteral, Disposition: Discharged}
		}
		return DispatchCheck{Kind: DispatchLiteral, Disposition: Inline}
	case *plan.KindDispatch:
		return DispatchCheck{Kind: DispatchJSONKind, Disposition: Inline}
	case *plan.PropertyDispatch:
		return DispatchCheck{Kind: DispatchProperty, Disposition: Inline}
	case *plan.PresenceDispatch:
		return DispatchCheck{Kind: DispatchPresence, Disposition: Inline}
	case *plan.PredicateCountDispatch:
		return DispatchCheck{Kind: DispatchPredicateCount, Disposition: Delegate}
	default:
		// Every variant is named above. A new one must be classified deliberately: the
		// zero value reads as "nothing to select", which is the one answer that cannot
		// be recovered from later.
		panic(fmt.Sprintf("gogen: unhandled plan.DispatchPlan variant %T", d))
	}
}

// foldedToEnum reports whether the literals reached the Go type, which is what makes a
// [plan.LiteralDispatch] discharged rather than outstanding.
func foldedToEnum(t GoType) bool {
	seen := make(map[GoType]bool)
	for !seen[t] {
		seen[t] = true
		switch x := t.(type) {
		case *Enum:
			return true
		case *Named:
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
