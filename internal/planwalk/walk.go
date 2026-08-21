// Package planwalk is the single structural traversal of the [plan] types (issue #47).
//
// [Fold] is the primitive: it threads an accumulator through every [Node] reachable from
// a plan and lets the callback choose, per node, whether to descend, skip the subtree or
// stop the walk. Each node carries the [Edge] it hangs off its parent by, which is what
// an instance-directed consumer needs to derive the sub-value a child applies to. The
// collectors below are [Fold] with a visitor that always descends.
//
// Every case of the traversal destructures its value into an anonymous struct and walks
// THAT binding rather than the original. Go assignability requires identical underlying
// types, so adding, renaming, retyping or reordering a field of a plan struct stops this
// package compiling, and the traversal cannot be repaired without naming the new field.
// The binding form is deliberate: a discard guard would compile identically while letting
// a new field be silenced without ever being walked. Those guards live in children.go.
//
// Interface variants are not covered by that mechanism, since Go has no sealed-interface
// exhaustiveness check. Each switch therefore ends in a panicking default, and
// [AllRepresentations], [AllDispatchPlans] and [AllPredicateExprs] give tests a list to
// drive every variant through it.
package planwalk

import (
	"github.com/ogen-go/schemacompiler/plan"
)

// Plan calls visit for every [plan.Representation] reachable from p: those of its own
// representation tree, and those of the plans nested in its dispatch branches and in the
// residual predicates that carry a plan.
//
// p.Resolution is bound but not descended: its definitions are whole-document plans that
// callers hold separately, and walking them here would double-visit them.
func Plan(p plan.CompilationPlan, visit func(plan.Representation)) {
	Fold(p, struct{}{}, func(acc struct{}, n Node) (struct{}, Action) {
		if n.Kind == NodeRepresentation {
			visit(n.Representation)
		}
		return acc, Descend
	})
}

// Representation calls visit for r and for every representation nested within it,
// in pre-order. Nesting crosses the whole plans an object field, an additional or
// pattern value and an array item carry, so their validation and dispatch are reached
// too. A nil r visits nothing.
func Representation(r plan.Representation, visit func(plan.Representation)) {
	if r == nil {
		return
	}
	FoldNode(RepresentationNode(r), struct{}{}, func(acc struct{}, n Node) (struct{}, Action) {
		if n.Kind == NodeRepresentation {
			visit(n.Representation)
		}
		return acc, Descend
	})
}

// Dispatch calls visit for every [plan.CompilationPlan] d selects between. A nil d
// visits nothing.
func Dispatch(d plan.DispatchPlan, visit func(plan.CompilationPlan)) {
	if d == nil {
		return
	}
	FoldNode(DispatchNode(d), struct{}{}, branchVisitor(visit))
}

// Validation calls visit for every [plan.CompilationPlan] carried by a residual
// predicate of v.
func Validation(v plan.ValidationPlan, visit func(plan.CompilationPlan)) {
	FoldNode(ValidationNode(v), struct{}{}, branchVisitor(visit))
}

// Predicate calls visit for every [plan.CompilationPlan] e carries. Most variants carry
// none. A nil e visits nothing.
func Predicate(e plan.PredicateExpr, visit func(plan.CompilationPlan)) {
	if e == nil {
		return
	}
	FoldNode(PredicateNode(e), struct{}{}, branchVisitor(visit))
}

// branchVisitor reports the nested plans of a fold root without entering them: these
// collectors hand the caller a plan, and it is the caller's business whether to walk it.
func branchVisitor(visit func(plan.CompilationPlan)) func(struct{}, Node) (struct{}, Action) {
	return func(acc struct{}, n Node) (struct{}, Action) {
		if n.Kind != NodePlan {
			return acc, Descend
		}
		visit(n.Plan)
		return acc, Skip
	}
}
