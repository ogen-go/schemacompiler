package planner

// gaps records why a plan admits values its schema rejects for reasons the plan's own
// structure does not show. [plan.Exactness] has no per-plan field to roll up through, so
// the builder accumulates these across one Build call and [exactnessOf] reads them.
//
// The two are not the same rung (design §24, issue #84): an asserted discriminator still
// leaves every branch's own validation in place to catch a mis-tagged instance, while a
// dropped constraint is closed by nothing at all.
type gaps struct {
	// asserted records that some dispatch anywhere in the plan trusts a declared
	// discriminator instead of proving disjointness ([plan.TagAsserted]).
	asserted bool
	// dropped records that a constraint was deliberately dropped somewhere in the plan
	// because enforcing it was not sound — today, a negation over an inexactly modeled
	// sub-schema (issue #82).
	dropped bool
}

// since returns the gaps g opened relative to before, so a sub-plan is judged on what
// building it introduced rather than on flags an enclosing or earlier sibling plan set.
func (g gaps) since(before gaps) gaps {
	return gaps{
		asserted: g.asserted && !before.asserted,
		dropped:  g.dropped && !before.dropped,
	}
}
