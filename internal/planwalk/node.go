package planwalk

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/plan"
)

// NodeKind names which of a [Node]'s payloads is set.
type NodeKind uint8

const (
	NodePlan NodeKind = iota
	NodeRepresentation
	NodeDispatch
	NodeValidation
	NodePredicate
)

var nodeKindNames = [...]string{
	NodePlan:           "plan",
	NodeRepresentation: "representation",
	NodeDispatch:       "dispatch",
	NodeValidation:     "validation",
	NodePredicate:      "predicate",
}

func (k NodeKind) String() string {
	if int(k) >= len(nodeKindNames) {
		return "node-kind(" + strconv.Itoa(int(k)) + ")"
	}
	return nodeKindNames[k]
}

// Node is one position in a plan: the value found there, plus the [Edge] that led to it
// from its parent. Kind says which payload is set; the others are zero.
//
// A node carries the plan value itself rather than a summary of it, so a consumer that
// needs a variant's payload (a bound, a pattern, a numeric domain) type-switches on it
// as usual. What the traversal contributes is the structure: which children a node has,
// and what each one's [Edge] is.
type Node struct {
	Kind NodeKind
	Edge Edge

	Plan           plan.CompilationPlan
	Representation plan.Representation
	Dispatch       plan.DispatchPlan
	Validation     plan.ValidationPlan
	Predicate      plan.PredicateExpr
}

// PlanNode returns p as a fold root.
func PlanNode(p plan.CompilationPlan) Node {
	return Node{Kind: NodePlan, Plan: p}
}

// RepresentationNode returns r as a fold root.
func RepresentationNode(r plan.Representation) Node {
	return Node{Kind: NodeRepresentation, Representation: r}
}

// DispatchNode returns d as a fold root.
func DispatchNode(d plan.DispatchPlan) Node {
	return Node{Kind: NodeDispatch, Dispatch: d}
}

// ValidationNode returns v as a fold root.
func ValidationNode(v plan.ValidationPlan) Node {
	return Node{Kind: NodeValidation, Validation: v}
}

// PredicateNode returns e as a fold root.
func PredicateNode(e plan.PredicateExpr) Node {
	return Node{Kind: NodePredicate, Predicate: e}
}
