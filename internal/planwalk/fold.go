package planwalk

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// Action is what a fold callback tells the traversal to do next.
type Action uint8

const (
	// Descend visits the node's children.
	Descend Action = iota
	// Skip leaves the node's children unvisited and continues with its next sibling.
	Skip
	// Stop ends the whole fold immediately; the accumulator is returned as it stands.
	Stop
)

var actionNames = [...]string{Descend: "descend", Skip: "skip", Stop: "stop"}

func (a Action) String() string {
	if int(a) >= len(actionNames) {
		return fmt.Sprintf("action(%d)", uint8(a))
	}
	return actionNames[a]
}

// Fold threads acc through every node reachable from p in pre-order, calling cb at each
// one and obeying the [Action] it returns.
//
// p.Resolution is not descended: its definitions are whole-document plans that callers
// hold separately, and folding them here would visit them once per reference.
func Fold[T any](p plan.CompilationPlan, acc T, cb func(T, Node) (T, Action)) T {
	return FoldNode(PlanNode(p), acc, cb)
}

// FoldNode is [Fold] rooted at an arbitrary node, for consumers that start from a
// representation, a dispatch, a validation plan or a predicate rather than a whole plan.
// The root is visited with whatever [Edge] its Node carries, so a fold can be re-rooted
// at a child without losing how that child hangs off its parent; [Children] is the
// one-level form.
func FoldNode[T any](root Node, acc T, cb func(T, Node) (T, Action)) T {
	acc, _ = foldNode(root, acc, cb)
	return acc
}

func foldNode[T any](n Node, acc T, cb func(T, Node) (T, Action)) (T, bool) {
	acc, action := cb(acc, n)
	switch action {
	case Skip:
		return acc, false
	case Stop:
		return acc, true
	case Descend:
	default:
		panic(fmt.Sprintf("planwalk: unknown Action %d", uint8(action)))
	}

	stopped := false
	children(n, func(child Node) bool {
		acc, stopped = foldNode(child, acc, cb)
		return !stopped
	})
	return acc, stopped
}
