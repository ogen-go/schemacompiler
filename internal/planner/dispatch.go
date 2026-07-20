package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// pushContext intersects an outer kind restriction and sibling context into one branch
// of a combinator (design §15.4, §17): `T ∩ ExactlyOne(A1,...,An) = ExactlyOne(T∩A1,
// ..., T∩An)`, and similarly for AnyOf.
func pushContext(k plan.KindSet, ctx components, op ir.Expr) ir.Expr {
	operands := append([]ir.Expr{op, ir.Kinds{Set: k, Numeric: ctx.numeric}}, ctx.nonKindOperands()...)
	return ir.All{Operands: operands}
}

// nonKindOperands reconstructs every non-kind contribution as sibling ir.Expr nodes.
func (c components) nonKindOperands() []ir.Expr {
	var out []ir.Expr
	if c.literal != nil {
		out = append(out, *c.literal)
	}
	for _, s := range c.shapes {
		out = append(out, ir.Shape{Detail: s})
	}
	for _, p := range c.predicates {
		out = append(out, p)
	}
	for _, n := range c.nots {
		out = append(out, n)
	}
	out = append(out, c.refs...)
	out = append(out, c.combinators...)
	return out
}

// discCase pairs a discriminator value with the (already context-augmented) branch
// expression it selects.
type discCase struct {
	Value any
	Expr  ir.Expr
}

// buildUnionWithContext builds the dispatch plan for one AnyOf/ExactlyOne combinator,
// after pushing the outer kind restriction and sibling constraints (ctx) into every
// branch (design §9, §18). isOneOf controls the PredicateCountDispatch fallback bounds.
func (b *builder) buildUnionWithContext(k plan.KindSet, combinator ir.Expr, ctx components, path string) plan.CompilationPlan {
	var operands []ir.Expr
	isOneOf := false
	switch v := combinator.(type) {
	case ir.AnyOf:
		operands = v.Operands
	case ir.ExactlyOne:
		operands = v.Operands
		isOneOf = true
	default:
		return b.build(combinator, path)
	}

	if len(operands) == 0 {
		// AnyOf()/ExactlyOne() == Never (design §15.1).
		return b.neverPlanAt(path)
	}
	if len(operands) == 1 {
		return b.build(pushContext(k, ctx, operands[0]), path)
	}

	branchExprs := make([]ir.Expr, len(operands))
	for i, op := range operands {
		branchExprs[i] = pushContext(k, ctx, op)
	}

	// Static discriminator analysis, in preference order (design §18).
	if cases, ok := literalCases(branchExprs); ok {
		return b.buildLiteralDispatch(cases, path)
	}
	if name, cases, ok := b.propertyDispatchCases(branchExprs); ok {
		return b.buildPropertyDispatch(name, cases, path)
	}
	if name, absent, present, ok := detectPresenceDispatch(branchExprs); ok {
		return b.buildPresenceDispatch(name, absent, present, path)
	}
	if pairwiseKindDisjoint(branchExprs) {
		return b.buildKindDisjointDispatch(branchExprs, path)
	}

	// Fallback: branches overlap and cannot be statically discriminated; runtime
	// predicate/match-count evaluation is required (design §9, §20.6). Representable,
	// but flagged per docs/implementation.md v1 scope.
	minimum, maximum := 1, len(branchExprs)
	if isOneOf {
		maximum = 1
	}
	b.diag(path, plan.SeverityWarning,
		"oneOf/anyOf branches overlap; requires runtime predicate-count validation")
	return b.buildPredicateCountDispatch(branchExprs, minimum, maximum, path)
}

// extractLiteral reports whether e is (after flattening) nothing more than a bare
// literal, with no other structural or predicate content.
func extractLiteral(e ir.Expr) (any, bool) {
	c := flattenAll([]ir.Expr{e})
	if c.never || c.literal == nil {
		return nil, false
	}
	if len(c.shapes) > 0 || len(c.predicates) > 0 || len(c.combinators) > 0 ||
		len(c.nots) > 0 || len(c.refs) > 0 {
		return nil, false
	}
	return c.literal.Value, true
}

// literalCases reports whether every branch is a bare literal (enum/const union,
// design §18 discriminator class 2), returning each branch's value.
func literalCases(branchExprs []ir.Expr) ([]discCase, bool) {
	cases := make([]discCase, len(branchExprs))
	seen := newValueSet(len(branchExprs))
	for i, e := range branchExprs {
		v, ok := extractLiteral(e)
		if !ok || !seen.add(v) {
			return nil, false
		}
		cases[i] = discCase{Value: v, Expr: e}
	}
	return cases, true
}

func (b *builder) buildLiteralDispatch(cases []discCase, path string) plan.CompilationPlan {
	lcases := make([]plan.LiteralCase, len(cases))
	alts := make([]plan.Representation, len(cases))
	capLevel := plan.StaticDispatch
	var resParts []plan.ResolutionPlan
	for i, c := range cases {
		sub := b.build(c.Expr, path)
		lcases[i] = plan.LiteralCase{Value: c.Value, Plan: sub}
		alts[i] = sub.Representation
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}
	return plan.CompilationPlan{
		Representation: unionRepresentation(alts),
		Dispatch:       plan.LiteralDispatch{Cases: lcases},
		Resolution:     mergeResolution(resParts...),
		Capability:     capLevel,
	}
}

// pairwiseKindDisjoint reports whether every pair of branches accepts disjoint JSON
// kinds, which is a sufficient proof of overall disjointness (design §15.3).
func pairwiseKindDisjoint(branchExprs []ir.Expr) bool {
	for i := 0; i < len(branchExprs); i++ {
		for j := i + 1; j < len(branchExprs); j++ {
			if branchExprs[i].Kinds()&branchExprs[j].Kinds() != 0 {
				return false
			}
		}
	}
	return true
}

func (b *builder) buildKindDisjointDispatch(branchExprs []ir.Expr, path string) plan.CompilationPlan {
	cases := make(map[plan.JSONKind]plan.CompilationPlan)
	var alts []plan.Representation
	capLevel := plan.StaticDispatch
	var resParts []plan.ResolutionPlan
	for _, be := range branchExprs {
		sub := b.build(be, path)
		alts = append(alts, sub.Representation)
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
		for _, kind := range splitKinds(be.Kinds()) {
			cases[kind] = sub
		}
	}
	return plan.CompilationPlan{
		Representation: unionRepresentation(alts),
		Dispatch:       plan.KindDispatch{Cases: cases},
		Resolution:     mergeResolution(resParts...),
		Capability:     capLevel,
	}
}

// discriminatorProperty reports whether e (after flattening) is an object branch that
// requires a specific literal-const value on some property (design §18.2).
func discriminatorProperty(e ir.Expr) (string, any, bool) {
	c := flattenAll([]ir.Expr{e})
	if c.never {
		return "", nil, false
	}
	required := make(map[string]bool)
	for _, p := range c.predicates {
		if rd, ok := p.Detail.(ir.RequiredDetail); ok {
			for _, name := range rd.Properties {
				required[name] = true
			}
		}
	}
	for _, sd := range c.shapes {
		os, ok := sd.(ir.ObjectShape)
		if !ok {
			continue
		}
		for _, prop := range os.Properties {
			if !required[prop.Name] {
				continue
			}
			if v, ok := extractLiteral(prop.Schema); ok {
				return prop.Name, v, true
			}
		}
	}
	return "", nil, false
}

// propertyDispatchCases reports whether every branch is discriminated by the same
// required property name, with pairwise-distinct literal values (design §18.2).
func (b *builder) propertyDispatchCases(branchExprs []ir.Expr) (string, []discCase, bool) {
	var propName string
	cases := make([]discCase, len(branchExprs))
	seen := newValueSet(len(branchExprs))
	for i, be := range branchExprs {
		name, val, ok := discriminatorProperty(be)
		if !ok {
			return "", nil, false
		}
		if i == 0 {
			propName = name
		} else if name != propName {
			return "", nil, false
		}
		if !seen.add(val) {
			return "", nil, false
		}
		cases[i] = discCase{Value: val, Expr: be}
	}
	return propName, cases, true
}

func (b *builder) buildPropertyDispatch(name string, cases []discCase, path string) plan.CompilationPlan {
	lcases := make([]plan.LiteralCase, len(cases))
	alts := make([]plan.Representation, len(cases))
	capLevel := plan.StaticDispatch
	var resParts []plan.ResolutionPlan
	for i, c := range cases {
		sub := b.build(c.Expr, path)
		lcases[i] = plan.LiteralCase{Value: c.Value, Plan: sub}
		alts[i] = sub.Representation
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}
	return plan.CompilationPlan{
		Representation: unionRepresentation(alts),
		Dispatch:       plan.PropertyDispatch{Property: name, Cases: lcases},
		Resolution:     mergeResolution(resParts...),
		Capability:     capLevel,
	}
}

// negatedRequiredSingle reports whether e (after flattening) is exactly Not(Has(name))
// for a single property name, and nothing else (the "absent" branch dependentSchemas
// desugars to, design §12.7).
func negatedRequiredSingle(e ir.Expr) (string, bool) {
	c := flattenAll([]ir.Expr{e})
	if c.never || len(c.nots) != 1 {
		return "", false
	}
	if len(c.shapes) > 0 || len(c.predicates) > 0 || len(c.combinators) > 0 ||
		len(c.refs) > 0 || c.literal != nil {
		return "", false
	}
	inner := flattenAll([]ir.Expr{c.nots[0].Operand})
	if inner.never || len(inner.predicates) != 1 {
		return "", false
	}
	rd, ok := inner.predicates[0].Detail.(ir.RequiredDetail)
	if !ok || len(rd.Properties) != 1 {
		return "", false
	}
	if len(inner.shapes) > 0 || len(inner.combinators) > 0 || len(inner.nots) > 0 ||
		len(inner.refs) > 0 || inner.literal != nil {
		return "", false
	}
	return rd.Properties[0], true
}

// requiredSingleHeld reports whether e's flattened predicates require name's presence.
func requiredSingleHeld(e ir.Expr, name string) bool {
	c := flattenAll([]ir.Expr{e})
	for _, p := range c.predicates {
		if rd, ok := p.Detail.(ir.RequiredDetail); ok {
			for _, n := range rd.Properties {
				if n == name {
					return true
				}
			}
		}
	}
	return false
}

// detectPresenceDispatch recognizes the two-branch AnyOf(Not(Has(p)), All(Has(p), S))
// shape that dependentSchemas desugars to (design §12.7), regardless of branch order.
func detectPresenceDispatch(branchExprs []ir.Expr) (name string, absent, present ir.Expr, ok bool) {
	if len(branchExprs) != 2 {
		return "", nil, nil, false
	}
	for _, order := range [2][2]int{{0, 1}, {1, 0}} {
		absentIdx, presentIdx := order[0], order[1]
		n, ok := negatedRequiredSingle(branchExprs[absentIdx])
		if !ok {
			continue
		}
		if requiredSingleHeld(branchExprs[presentIdx], n) {
			return n, branchExprs[absentIdx], branchExprs[presentIdx], true
		}
	}
	return "", nil, nil, false
}

func (b *builder) buildPresenceDispatch(name string, absentExpr, presentExpr ir.Expr, path string) plan.CompilationPlan {
	absent := b.build(absentExpr, path)
	present := b.build(presentExpr, path)
	capLevel := maxCapability(plan.StaticDispatch, maxCapability(present.Capability, absent.Capability))
	return plan.CompilationPlan{
		Representation: unionRepresentation([]plan.Representation{present.Representation, absent.Representation}),
		Dispatch:       plan.PresenceDispatch{Property: name, Present: present, Absent: absent},
		Resolution:     mergeResolution(present.Resolution, absent.Resolution),
		Capability:     capLevel,
	}
}

func (b *builder) buildPredicateCountDispatch(branchExprs []ir.Expr, minimum, maximum int, path string) plan.CompilationPlan {
	branches := make([]plan.CompilationPlan, len(branchExprs))
	var alts []plan.Representation
	capLevel := plan.PredicateDispatch
	var resParts []plan.ResolutionPlan
	for i, be := range branchExprs {
		sub := b.build(be, path)
		branches[i] = sub
		alts = append(alts, sub.Representation)
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}
	return plan.CompilationPlan{
		// Sound over-approximation: the exact branch is chosen by match-count at
		// runtime, so the representation must be able to hold every branch's values.
		Representation: unionRepresentation(alts),
		Dispatch:       plan.PredicateCountDispatch{Branches: branches, Minimum: minimum, Maximum: maximum},
		Resolution:     mergeResolution(resParts...),
		Capability:     capLevel,
	}
}
