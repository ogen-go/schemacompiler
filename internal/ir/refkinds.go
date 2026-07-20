package ir

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// refTargetKinds computes the kind summary of a resolved $ref target (design §6) so a
// [Ref] can carry it (see expr.go). It over-approximates soundly: a reference cycle or an
// unresolved target yields [plan.SetAny], so disjointness is only ever proven, never
// wrongly assumed.
func refTargetKinds(n *frontend.Node) plan.KindSet {
	if n.Resolved == nil {
		return plan.SetAny
	}
	a := kindAnalyzer{
		memo:   map[*frontend.Node]plan.KindSet{},
		inProg: map[*frontend.Node]bool{},
	}
	return a.of(n.Resolved)
}

// kindAnalyzer computes a cycle-safe kind summary over the frontend AST. It memoizes
// per node and, on re-entry into a node still being computed (a reference cycle), returns
// SetAny — the sound over-approximation that keeps the walk finite.
type kindAnalyzer struct {
	memo   map[*frontend.Node]plan.KindSet
	inProg map[*frontend.Node]bool
}

func (a kindAnalyzer) of(n *frontend.Node) plan.KindSet {
	if n == nil {
		return plan.SetAny // absent sub-schema behaves as `true`.
	}
	if k, ok := a.memo[n]; ok {
		return k
	}
	if a.inProg[n] {
		return plan.SetAny // cycle: conservative.
	}
	a.inProg[n] = true
	k := a.compute(n)
	delete(a.inProg, n)
	a.memo[n] = k
	return k
}

// compute intersects the kind restrictions the node's sibling keywords impose (design
// §6.1); keywords that do not restrict kinds (predicates, shapes, not, dynamicRef)
// contribute SetAny and drop out of the intersection.
func (a kindAnalyzer) compute(n *frontend.Node) plan.KindSet {
	if n.Always != nil {
		if *n.Always {
			return plan.SetAny
		}
		return 0
	}

	set := plan.SetAny
	if n.Ref != "" {
		set &= a.of(n.Resolved)
	}
	if n.HasType {
		set &= plan.KindSet(n.Types)
	}
	if n.Const != nil {
		set &= literalKind(n.Const.Decoded)
	}
	if len(n.Enum) > 0 {
		var u plan.KindSet
		for _, v := range n.Enum {
			u |= literalKind(v.Decoded)
		}
		set &= u
	}
	for _, sub := range n.AllOf {
		set &= a.of(sub)
	}
	if len(n.AnyOf) > 0 {
		set &= a.unionOf(n.AnyOf)
	}
	if len(n.OneOf) > 0 {
		set &= a.unionOf(n.OneOf)
	}
	return set
}

func (a kindAnalyzer) unionOf(nodes []*frontend.Node) plan.KindSet {
	var u plan.KindSet
	for _, sub := range nodes {
		u |= a.of(sub)
	}
	return u
}
