package plan

// ValidationPlan is the residual predicate that the Go type cannot enforce (design §8).
type ValidationPlan struct {
	Predicates []GuardedPredicate
}

// Empty reports whether there is nothing to validate.
func (p ValidationPlan) Empty() bool { return len(p.Predicates) == 0 }

// GuardedPredicate is a check scoped to a set of JSON kinds (design §3.1, §8).
// Applicability is read one of two ways, and Assert says which:
//
//	Assert == false   guard      an instance outside the set contributes nothing
//	Assert == true    assertion  an instance outside the set is rejected
//
// `minLength: 5` is a guard: a number is not a short string, it is simply not a string.
// `type: "string"` is an assertion, and is the one case that carries no Expression — the
// kind check is the whole of it. Every other predicate has one.
type GuardedPredicate struct {
	Applicability KindSet
	Assert        bool
	Expression    PredicateExpr
}

// PredicateExpr is a residual runtime check. The concrete variants (length bounds,
// numeric ranges, pattern match, required presence, uniqueness, match-count, ...) are
// defined by the planner; a backend switches over them to emit validator code.
//
// Variants implement this on a pointer receiver, so only *T satisfies it. A value
// receiver would put the method in both T's and *T's method set, leaving every type
// switch to match one spelling and silently miss the other (issue #133).
type PredicateExpr interface {
	isPredicateExpr()
}
