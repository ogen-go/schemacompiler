package planwalk

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// NilPlanSlot is one slot left nil where the plan contract says a plan is always stated.
type NilPlanSlot struct {
	// Owner is the variant holding the slot, and Slot its field name.
	Owner plan.Representation
	Pred  plan.PredicateExpr
	Slot  string
}

func (s NilPlanSlot) String() string {
	owner := fmt.Sprintf("%T", s.Owner)
	if s.Owner == nil {
		owner = fmt.Sprintf("%T", s.Pred)
	}
	return owner + "." + s.Slot
}

// NilPlanSlots returns every slot in p where a sub-plan is absent.
//
// The planner states every one of them outright: `additionalProperties` absent becomes a
// plan over [plan.AnyRepresentation] and `false` becomes one over
// [plan.NeverRepresentation], so "no plan here" is never how either is spelled. The same
// holds for an array's rest. Nil is therefore not a third value with a meaning of its own
// — it is a slot the planner failed to fill, and a consumer reading one has no way to tell
// which of the two it stood for.
//
// This is enforced rather than documented because the contract was documented for years
// and never held anything: across the ogen corpus and the JSON-Schema-Test-Suite together,
// all four slots are nil zero times in 12354 occurrences. A rule nothing exercises is a
// rule nobody notices breaking.
func NilPlanSlots(p plan.CompilationPlan) []NilPlanSlot {
	return Fold(p, []NilPlanSlot(nil), func(acc []NilPlanSlot, n Node) ([]NilPlanSlot, Action) {
		switch r := n.Representation.(type) {
		case *plan.ObjectRepresentation:
			if r.Additional == nil {
				acc = append(acc, NilPlanSlot{Owner: r, Slot: "Additional"})
			}
		case *plan.ArrayRepresentation:
			if r.Rest.Plan.Representation == nil {
				acc = append(acc, NilPlanSlot{Owner: r, Slot: "Rest.Plan.Representation"})
			}
		}
		switch e := n.Predicate.(type) {
		case *plan.ObjectStructurePredicate:
			if e.Additional == nil {
				acc = append(acc, NilPlanSlot{Pred: e, Slot: "Additional"})
			}
		case *plan.ArrayStructurePredicate:
			if e.Rest == nil {
				acc = append(acc, NilPlanSlot{Pred: e, Slot: "Rest"})
			}
		}
		return acc, Descend
	})
}

// UndispatchedUnions returns every plan in p whose representation is a
// [plan.UnionRepresentation] with no dispatch to select among the alternatives.
//
// [plan.UnionRepresentation.Alternatives] is `[]plan.Representation`, not
// `[]plan.CompilationPlan`: it is the storage shape alone, and carries no validation,
// capability or resolution for a branch. Those live in the plan's [plan.DispatchPlan],
// which is what a backend lowering a sum type must read. A union with a
// [plan.NoDispatch] would therefore be a type with alternatives nothing can choose
// between — never produced (0 of 1211 across both corpora), and unlowerable if it were.
func UndispatchedUnions(p plan.CompilationPlan) []plan.CompilationPlan {
	return Fold(p, []plan.CompilationPlan(nil), func(acc []plan.CompilationPlan, n Node) ([]plan.CompilationPlan, Action) {
		if n.Kind != NodePlan {
			return acc, Descend
		}
		if _, union := n.Plan.Representation.(*plan.UnionRepresentation); !union {
			return acc, Descend
		}
		if _, none := n.Plan.Dispatch.(*plan.NoDispatch); none {
			acc = append(acc, n.Plan)
		}
		return acc, Descend
	})
}

// ContractViolations reports every plan-shape rule p breaks, as one line each. It is the
// aggregate the corpus walks assert on: an empty result is the contract holding.
func ContractViolations(p plan.CompilationPlan) []string {
	var out []string
	for _, s := range NilPlanSlots(p) {
		out = append(out, "unstated sub-plan: "+s.String())
	}
	for range UndispatchedUnions(p) {
		out = append(out, "union representation with no dispatch to select an alternative")
	}
	for _, g := range OverbroadGuards(p) {
		out = append(out, fmt.Sprintf("guard %v is wider than %T reads (%v)",
			plan.KindSetNames(g.Guard), g.Predicate, plan.KindSetNames(g.Meaning)))
	}
	return out
}
