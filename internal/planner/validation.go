package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// mappedPredicate is one residual predicate lowered from ir, plus any capability/
// resolution contribution its nested sub-schema (contains, propertyNames) rolls up.
type mappedPredicate struct {
	Expr       plan.PredicateExpr
	Capability plan.CapabilityLevel
	Resolution plan.ResolutionPlan
}

// mapPredicate lowers one ir.Predicate's detail into a plan.PredicateExpr (design §8),
// mirroring ir's PredicateDetail set 1:1. Predicates whose detail embeds a sub-schema
// (contains, propertyNames) recursively build that sub-schema's plan and roll its
// capability/resolution into the result.
func (b *builder) mapPredicate(p ir.Predicate, path string) mappedPredicate {
	switch d := p.Detail.(type) {
	case ir.DroppedKeywordDetail:
		// Not a check: the keyword was left absent because its declared value was not one
		// the spec admits, so the plan accepts instances the schema rejects and nothing in
		// it closes the gap. The frontend already reported the keyword and where it was
		// written ([frontend.InvalidKeyword]); all that is owed here is the exactness rung.
		b.dropped = true
		return mappedPredicate{}
	case ir.MinLengthDetail:
		return mappedPredicate{Expr: plan.MinLengthPredicate{Value: d.Value}}
	case ir.MaxLengthDetail:
		return mappedPredicate{Expr: plan.MaxLengthPredicate{Value: d.Value}}
	case ir.PatternDetail:
		b.requireECMARegex(d.Regex, path, "pattern")
		return mappedPredicate{Expr: plan.PatternPredicate{Regex: d.Regex}}
	case ir.FormatDetail:
		return mappedPredicate{Expr: plan.FormatPredicate{Format: d.Format}}
	case ir.MinimumDetail:
		return mappedPredicate{Expr: plan.MinimumPredicate{Value: d.Value}}
	case ir.MaximumDetail:
		return mappedPredicate{Expr: plan.MaximumPredicate{Value: d.Value}}
	case ir.ExclusiveMinimumDetail:
		return mappedPredicate{Expr: plan.MinimumPredicate{Value: d.Value, Exclusive: true}}
	case ir.ExclusiveMaximumDetail:
		return mappedPredicate{Expr: plan.MaximumPredicate{Value: d.Value, Exclusive: true}}
	case ir.MultipleOfDetail:
		return mappedPredicate{Expr: plan.MultipleOfPredicate{Value: d.Value}}
	case ir.MinItemsDetail:
		return mappedPredicate{Expr: plan.MinItemsPredicate{Value: d.Value}}
	case ir.MaxItemsDetail:
		return mappedPredicate{Expr: plan.MaxItemsPredicate{Value: d.Value}}
	case ir.UniqueItemsDetail:
		// Distinctness is defined on JSON values, so it is decided before decoding or not
		// at all: two JSON-distinct items that decode to the same Go value would make this
		// reject an array the schema accepts (design §24.3).
		b.require(&b.reqs.JSONEquality, path, "uniqueItems")
		b.require(&b.reqs.RawEvaluation, path, "uniqueItems")
		return mappedPredicate{Expr: plan.UniqueItemsPredicate{}}
	case ir.ContainsDetail:
		sub := b.build(d.Schema, path+"/contains")
		minCount := uint64(1)
		if d.Min != nil {
			minCount = *d.Min
		}
		// contains/minContains/maxContains match-counting is representable but flagged
		// (docs/implementation.md v1 scope): keep the plan, note the runtime cost.
		b.diag(path, plan.SeverityWarning,
			"contains/minContains/maxContains requires runtime match-count validation")
		return mappedPredicate{
			Expr:       plan.ContainsCountPredicate{Schema: sub, Min: minCount, Max: d.Max},
			Capability: maxCapability(plan.PredicateDispatch, sub.Capability),
			Resolution: sub.Resolution,
		}
	case ir.RequiredDetail:
		return mappedPredicate{Expr: plan.RequiredPredicate{Properties: d.Properties}}
	case ir.MinPropertiesDetail:
		b.require(&b.reqs.RawEvaluation, path, "minProperties counts undeclared properties")
		return mappedPredicate{Expr: plan.MinPropertiesPredicate{Value: d.Value}}
	case ir.MaxPropertiesDetail:
		b.require(&b.reqs.RawEvaluation, path, "maxProperties counts undeclared properties")
		return mappedPredicate{Expr: plan.MaxPropertiesPredicate{Value: d.Value}}
	case ir.DependentRequiredDetail:
		entries := make([]plan.DependentRequiredEntry, len(d.Entries))
		for i, e := range d.Entries {
			entries[i] = plan.DependentRequiredEntry{Property: e.Property, Requires: e.Requires}
		}
		return mappedPredicate{Expr: plan.DependentRequiredPredicate{Entries: entries}}
	case ir.PropertyNamesDetail:
		b.require(&b.reqs.RawEvaluation, path, "propertyNames sees undeclared property names")
		sub := b.build(d.Schema, path+"/propertyNames")
		return mappedPredicate{
			Expr:       plan.PropertyNamesPredicate{Schema: sub},
			Capability: sub.Capability,
			Resolution: sub.Resolution,
		}
	default:
		b.diag(path, plan.SeverityWarning, "unrecognized predicate detail, dropped")
		return mappedPredicate{}
	}
}
