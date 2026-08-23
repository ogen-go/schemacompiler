package planner

import (
	"maps"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// refsTrusted reports whether every static reference reachable from p resolves to a target
// whose own plan reproduces its schema's accepted set exactly (design §10.1, §24).
//
// [buildRef] plans a reference from its identity alone, because the target's plan is
// assembled by a separate [BuildAt] call, so a reference reads as exactly modeled
// whatever its target turns out to be. Every consumer of a whole document already sees
// through that — the document assembly reports the diagnostics of the root and every
// reachable target — but a query inside one Build call, such as the negation gate in
// [withResidualNegation], does not, and there the optimism rejects valid instances (#82).
func (b *builder) refsTrusted(p plan.CompilationPlan, seen map[plan.SchemaID]bool) bool {
	trusted := true
	planwalk.Plan(p, func(r plan.Representation) {
		ref, isRef := r.(*plan.ReferenceRepresentation)
		if !isRef || !trusted {
			return
		}
		trusted = b.refTrusted(plan.SchemaID(ref.Name), seen)
	})
	return trusted
}

// refTrusted answers [refsTrusted] for one target, memoized per Build call.
//
// It is conservative wherever it cannot see: a registry that is absent (hand-built test
// fixtures, where refs are not followed at all), a target no longer in the reference index,
// a recursive target (design §19 — the walk would not terminate on its own, and the target's
// plan is a knot rather than a value the gate can judge) and a cycle the seen set catches
// all report false, which only ever drops a negation that might have been sound.
func (b *builder) refTrusted(target plan.SchemaID, seen map[plan.SchemaID]bool) bool {
	if trusted, ok := b.refTrust[target]; ok {
		return trusted
	}
	if seen[target] {
		return false
	}
	node, ok := b.refTargets()[string(target)]
	if !ok {
		return false
	}
	if b.recur[target] != frontend.NotRecursive {
		return false
	}

	next := make(map[plan.SchemaID]bool, len(seen)+1)
	maps.Copy(next, seen)
	next[target] = true

	// The target is rebuilt purely to inspect it, so everything the build accumulated is
	// rolled back: its diagnostics would otherwise be re-emitted once per reference site,
	// and its gaps would make the referring plan read as incomplete (design §25).
	diags, before := b.diags, b.gaps
	sub := b.build(ir.Compile(node), "")
	trusted := exactlyModeled(sub, b.since(before)) && b.refsTrusted(sub, next)
	b.diags, b.gaps = diags, before

	if b.refTrust == nil {
		b.refTrust = make(map[plan.SchemaID]bool)
	}
	b.refTrust[target] = trusted
	return trusted
}
