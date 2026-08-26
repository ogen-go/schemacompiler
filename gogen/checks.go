package gogen

import (
	"github.com/ogen-go/schemacompiler/plan"
)

// Checks is a plan's validation split by what the chosen Go type can carry.
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
}

// Split classifies every predicate of v against t.
//
// It reads v.Predicates rather than [plan.CompilationPlan.ResidualChecks], and that is the
// point of the pass. `ResidualChecks` discharges against the plan's own representation, so
// it answers for a shape `Lower` did not choose and goes stale the moment it chooses
// another — a `patternProperties` object is reported as needing nothing at all while the
// Go type enforces none of it (docs/backend.md §8).
func Split(t GoType, v plan.ValidationPlan) Checks {
	var c Checks
	for _, gp := range v.Predicates {
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
func (c Checks) Empty() bool { return len(c.Inline) == 0 && len(c.Delegate) == 0 }
