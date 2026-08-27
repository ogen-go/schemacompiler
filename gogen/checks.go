package gogen

import (
	"github.com/ogen-go/schemacompiler/plan"
)

// Checks is a plan's validation split by what the chosen Go type can carry, plus what the
// type left undone about the plan's branch selection.
//
// Discharged predicates appear in neither slot: the type states them, and restating them
// would be work with no instance to reject.
type Checks struct {
	// Inline is decidable from the decoded value. A structural check is inlined by
	// recursing into the sub-plans it carries, which are the plans the nested types were
	// lowered from.
	Inline []plan.GuardedPredicate
	// Delegate needs the raw JSON document and must be compiled and run against it.
	Delegate []plan.GuardedPredicate
	// Dispatch is what the type left to do about [plan.CompilationPlan.Dispatch]. It is
	// a fourth concern of a plan rather than a predicate, so it has a slot of its own:
	// without one a backend reads no delegated predicate and concludes nothing is
	// missing while the sum decodes as `any` (issue #155, docs/backend.md §14).
	Dispatch DispatchCheck
}

// Split classifies every predicate of p against t, and p's dispatch with them.
//
// It reads p.Validation.Predicates rather than [plan.CompilationPlan.ResidualChecks], and
// that is the point of the pass. `ResidualChecks` discharges against the plan's own
// representation, so it answers for a shape `Lower` did not choose and goes stale the
// moment it chooses another — a `patternProperties` object is reported as needing nothing
// at all while the Go type enforces none of it (docs/backend.md §8).
func Split(t GoType, p plan.CompilationPlan) Checks {
	c := Checks{Dispatch: ClassifyDispatch(t, p.Dispatch)}
	for _, gp := range p.Validation.Predicates {
		switch Classify(t, gp) {
		case Discharged:
		case Inline:
			c.Inline = append(c.Inline, gp)
		case Delegate:
			c.Delegate = append(c.Delegate, gp)
		}
	}
	return c
}

// Empty reports whether the type needs no validator at all, which is what design §22
// makes [plan.DirectGoType] mean.
func (c Checks) Empty() bool {
	return len(c.Inline) == 0 && len(c.Delegate) == 0 && c.Dispatch.Disposition == Discharged
}
