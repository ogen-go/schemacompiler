package plan

// ValidationPlan is the residual predicate that the Go type cannot enforce (design §8).
type ValidationPlan struct {
	Predicates []GuardedPredicate
}

// Empty reports whether there is nothing to validate.
func (p ValidationPlan) Empty() bool { return len(p.Predicates) == 0 }

// GuardedPredicate is a predicate that applies only to values of the given kinds
// (design §3, §8). A type-specific keyword such as minLength has Applicability
// SetString: non-strings pass vacuously.
type GuardedPredicate struct {
	Applicability KindSet
	Expression    PredicateExpr
}

// PredicateExpr is a residual runtime check. The concrete variants (length bounds,
// numeric ranges, pattern match, required presence, uniqueness, match-count, ...) are
// defined by the planner; a backend switches over them to emit validator code.
type PredicateExpr interface {
	isPredicateExpr()
}
