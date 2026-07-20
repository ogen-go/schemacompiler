package plan

// DispatchPlan selects among a finite set of known alternatives (design §9). It is
// distinct from schema resolution (design §2.4): dispatch is runtime branch selection
// over statically known plans.
type DispatchPlan interface {
	isDispatchPlan()
}

// NoDispatch is a single representation with no branch selection.
type NoDispatch struct{}

// KindDispatch selects a branch by the instance's JSON kind (design §18.1).
type KindDispatch struct {
	Cases map[JSONKind]CompilationPlan
}

// LiteralDispatch selects a branch by the instance's literal value (enum/const union).
type LiteralDispatch struct {
	Cases []LiteralCase
}

// LiteralCase pairs a comparable JSON literal with its plan. Value uses the JSON
// canonical Go form (float64 for numbers, etc.); it is a slice rather than a map so
// non-hashable literals (null, and by-value equality) are handled uniformly.
type LiteralCase struct {
	Value any
	Plan  CompilationPlan
}

// PropertyDispatch selects a branch by the value of a discriminator property
// (tagged union, design §18.2).
type PropertyDispatch struct {
	Property string
	Cases    []LiteralCase
}

// PresenceDispatch selects a branch by whether a property is present (design §12.7).
type PresenceDispatch struct {
	Property string
	Present  CompilationPlan
	Absent   CompilationPlan
}

// PredicateCountDispatch is the fallback for overlapping branches: evaluate each and
// require the number of matches to fall in [Minimum, Maximum] (design §9, §20.6).
// oneOf → [1,1]; anyOf → [1, len(Branches)].
type PredicateCountDispatch struct {
	Branches []CompilationPlan
	Minimum  int
	Maximum  int
}

func (NoDispatch) isDispatchPlan()             {}
func (KindDispatch) isDispatchPlan()           {}
func (LiteralDispatch) isDispatchPlan()        {}
func (PropertyDispatch) isDispatchPlan()       {}
func (PresenceDispatch) isDispatchPlan()       {}
func (PredicateCountDispatch) isDispatchPlan() {}
