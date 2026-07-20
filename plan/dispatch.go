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
// non-hashable literals (null, and by-value equality) are handled uniformly. Raw is the
// exact JSON source bytes of the literal, preserved so a backend can emit numbers past
// float64's precision (integers > 2^53, exact decimals) losslessly; it is nil for
// literals synthesized without source bytes, in which case Value is authoritative.
type LiteralCase struct {
	Value any
	Raw   []byte
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

// PredicateCountDispatch is the fallback for overlapping branches that no static
// discriminator can separate: the accepted branch is decided at runtime by trial
// validation, not by a structural tag (design §9, §20.6).
//
// Lowering contract. A backend runs every branch's full [CompilationPlan] —
// representation decode and residual validation — against the instance and counts the
// branches that accept it (a "match"). Letting k be that count, the instance is valid iff
//
//	Minimum <= k <= Maximum
//
// with oneOf → [1,1] (exactly one branch) and anyOf → [1, len(Branches)] (at least one, up
// to all). No branch may be skipped on a static guess: the branches overlap by
// construction, which is why static dispatch did not apply. The decoded value is held by
// the enclosing [UnionRepresentation] over the branches (a sound over-approximation, design
// §24); for oneOf the single accepting branch's representation is authoritative. A backend
// that cannot emit runtime match-counting MUST refuse and surface the plan's
// PredicateDispatch diagnostic rather than drop the schema or approximate it with a static
// discriminator (docs/integration.md §3, §6).
type PredicateCountDispatch struct {
	// Branches are the alternative plans, each trial-validated independently.
	Branches []CompilationPlan
	// Minimum and Maximum bound how many branches a valid instance may match.
	Minimum int
	Maximum int
}

func (NoDispatch) isDispatchPlan()             {}
func (KindDispatch) isDispatchPlan()           {}
func (LiteralDispatch) isDispatchPlan()        {}
func (PropertyDispatch) isDispatchPlan()       {}
func (PresenceDispatch) isDispatchPlan()       {}
func (PredicateCountDispatch) isDispatchPlan() {}
