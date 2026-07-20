package planner

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// unionRepresentation collapses a single alternative to itself, and wraps two or more
// into a UnionRepresentation (design §7).
func unionRepresentation(alts []plan.Representation) plan.Representation {
	if len(alts) == 1 {
		return alts[0]
	}
	return plan.UnionRepresentation{Alternatives: alts}
}

// buildKindRestricted infers the representation for an expression already narrowed to
// kind set k (design §7): a single kind lowers directly; multiple kinds fan out into a
// per-kind KindDispatch (design §18.1), since a backend needs a runtime kind check to
// pick which alternative to decode into.
func (b *builder) buildKindRestricted(k plan.KindSet, c components, path string) plan.CompilationPlan {
	bits := splitKinds(k)
	if len(bits) == 0 {
		return b.neverPlanAt(path)
	}
	if k == plan.SetAny && !c.hasKindRestriction {
		// No `type` keyword at all: every kind is possible not because it was asserted,
		// but because nothing restricts it (design §3). Fanning this out into a
		// per-kind KindDispatch would be sound but absurd (design §20.3's bare
		// `{"minLength": 3}` example must widen to Any, not a 6-way kind switch).
		return b.buildUnrestricted(c, path)
	}
	if len(bits) == 1 {
		return b.buildLeaf(bits[0], c, path)
	}

	cases := make(map[plan.JSONKind]plan.CompilationPlan, len(bits))
	alts := make([]plan.Representation, 0, len(bits))
	capLevel := plan.StaticDispatch
	var resParts []plan.ResolutionPlan
	for _, kind := range bits {
		sub := b.buildLeaf(kind, c, path)
		cases[kind] = sub
		alts = append(alts, sub.Representation)
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}
	return plan.CompilationPlan{
		Representation: unionRepresentation(alts),
		Dispatch:       plan.KindDispatch{Cases: cases},
		Resolution:     mergeResolution(resParts...),
		Capability:     capLevel,
	}
}

// buildUnrestricted builds the plan for an expression with no `type` restriction at
// all (design §3, §20.3): the representation must widen to Any since every kind is
// still possible, while guarded predicates remain exact (each fires only for its own
// kind at runtime). Object/array shape keywords without a guaranteed object/array
// context still contribute their v1-unsupported flags (unevaluatedProperties/Items),
// since those apply whenever the instance happens to be that kind, but do not
// contribute an ObjectRepresentation/ArrayRepresentation (design §12.1: `properties`
// alone must not become a struct).
func (b *builder) buildUnrestricted(c components, path string) plan.CompilationPlan {
	var val plan.ValidationPlan
	capLevel := plan.DirectGoType
	var resParts []plan.ResolutionPlan
	for _, p := range c.predicates {
		m := b.mapPredicate(p, path)
		if m.Expr != nil {
			val.Predicates = append(val.Predicates, plan.GuardedPredicate{Applicability: p.Guard, Expression: m.Expr})
		}
		capLevel = maxCapability(capLevel, m.Capability)
		if m.Resolution != nil {
			resParts = append(resParts, m.Resolution)
		}
	}

	merged := mergeObjectShapes(c.shapes)
	if merged.unevaluated {
		b.diag(path, plan.SeverityError, "unevaluatedProperties requires evaluated-property tracking")
		capLevel = maxCapability(capLevel, plan.EvaluationStateValidation)
	}
	if _, _, unevaluatedItems := mergeArrayShapes(c.shapes); unevaluatedItems {
		b.diag(path, plan.SeverityError, "unevaluatedItems requires evaluated-item tracking")
		capLevel = maxCapability(capLevel, plan.EvaluationStateValidation)
	}
	if len(c.nots) > 0 {
		b.diag(path, plan.SeverityInfo, "not: residual negation not enforced by the v1 validator")
	}

	rep := plan.Representation(plan.AnyRepresentation{})
	var disp plan.DispatchPlan = plan.NoDispatch{}
	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}

// buildLeaf builds the plan for a single, already-decided JSON kind. Any surviving
// combinator siblings still need the kind pushed down into their branches first.
func (b *builder) buildLeaf(kind plan.JSONKind, c components, path string) plan.CompilationPlan {
	if len(c.combinators) > 0 {
		primary := c.combinators[0]
		rest := c
		rest.combinators = append([]ir.Expr{}, c.combinators[1:]...)
		return b.buildUnionWithContext(kindBit(kind), primary, rest, path)
	}
	switch kind {
	case plan.KindObject:
		return b.buildObject(c, path)
	case plan.KindArray:
		return b.buildArray(c, path)
	default:
		return b.buildScalar(kind, c, path)
	}
}

func (b *builder) buildScalar(kind plan.JSONKind, c components, path string) plan.CompilationPlan {
	guard := kindBit(kind)
	var val plan.ValidationPlan
	capLevel := plan.DirectGoType
	var resParts []plan.ResolutionPlan
	for _, p := range c.predicates {
		if p.Guard&guard == 0 {
			continue // vacuous for this kind: the guard never fires, safe to drop.
		}
		m := b.mapPredicate(p, path)
		if m.Expr != nil {
			val.Predicates = append(val.Predicates, plan.GuardedPredicate{Applicability: guard, Expression: m.Expr})
		}
		capLevel = maxCapability(capLevel, m.Capability)
		if m.Resolution != nil {
			resParts = append(resParts, m.Resolution)
		}
	}

	rep := plan.Representation(plan.PrimitiveRepresentation{Kind: kind, Numeric: c.numeric})
	var disp plan.DispatchPlan = plan.NoDispatch{}
	if c.literal != nil && literalKind(c.literal.Value) == kind {
		disp = plan.LiteralDispatch{Cases: []plan.LiteralCase{{
			Value: c.literal.Value,
			Plan: plan.CompilationPlan{
				Representation: rep,
				Dispatch:       plan.NoDispatch{},
				Resolution:     plan.FullyResolved{},
				Capability:     plan.DirectGoType,
			},
		}}}
	}
	if len(c.nots) > 0 {
		b.diag(path, plan.SeverityInfo, "not: residual negation not enforced by the v1 validator")
	}

	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}

// buildLiteral builds the plan for a bare literal (const), lowered as a single-case
// LiteralDispatch (design §18 discriminator class 2) so the exact value is enforced by
// dispatch rather than left unchecked in an over-broad primitive representation.
func (b *builder) buildLiteral(v ir.Literal, _ string) plan.CompilationPlan {
	kind := literalKind(v.Value)
	rep := plan.Representation(plan.PrimitiveRepresentation{Kind: kind})
	branch := plan.CompilationPlan{
		Representation: rep,
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.FullyResolved{},
		Capability:     plan.DirectGoType,
	}
	return plan.CompilationPlan{
		Representation: rep,
		Dispatch:       plan.LiteralDispatch{Cases: []plan.LiteralCase{{Value: v.Value, Plan: branch}}},
		Resolution:     plan.FullyResolved{},
		Capability:     plan.StaticDispatch,
	}
}

// mergedObject is the result of intersecting every ObjectShape sibling found in an All
// (design §12.3: allOf-composed object constraints intersect).
type mergedObject struct {
	properties           map[string]ir.Expr
	order                []string
	patternProperties    []ir.PatternPropertyExpr
	additionalProperties ir.Expr
	unevaluated          bool
}

func mergeObjectShapes(shapes []ir.ShapeDetail) mergedObject {
	m := mergedObject{properties: make(map[string]ir.Expr)}
	for _, sd := range shapes {
		os, ok := sd.(ir.ObjectShape)
		if !ok {
			continue
		}
		for _, p := range os.Properties {
			if existing, ok := m.properties[p.Name]; ok {
				m.properties[p.Name] = ir.All{Operands: []ir.Expr{existing, p.Schema}}
			} else {
				m.properties[p.Name] = p.Schema
				m.order = append(m.order, p.Name)
			}
		}
		m.patternProperties = append(m.patternProperties, os.PatternProperties...)
		if os.AdditionalProperties != nil {
			if m.additionalProperties == nil {
				m.additionalProperties = os.AdditionalProperties
			} else {
				m.additionalProperties = ir.All{Operands: []ir.Expr{m.additionalProperties, os.AdditionalProperties}}
			}
		}
		if os.UnevaluatedProperties != nil {
			m.unevaluated = true
		}
	}
	return m
}

// buildObject infers an ObjectRepresentation (design §7, §12): fields carry the
// three-state presence/nullable model (§7.1, §12.2), independent of each other.
func (b *builder) buildObject(c components, path string) plan.CompilationPlan {
	merged := mergeObjectShapes(c.shapes)
	required := make(map[string]bool)
	var val plan.ValidationPlan
	capLevel := plan.DirectGoType
	var resParts []plan.ResolutionPlan

	for _, p := range c.predicates {
		if p.Guard&plan.SetObject == 0 {
			continue
		}
		if rd, ok := p.Detail.(ir.RequiredDetail); ok {
			for _, name := range rd.Properties {
				required[name] = true
			}
		}
		m := b.mapPredicate(p, path)
		if m.Expr != nil {
			val.Predicates = append(val.Predicates, plan.GuardedPredicate{Applicability: plan.SetObject, Expression: m.Expr})
		}
		capLevel = maxCapability(capLevel, m.Capability)
		if m.Resolution != nil {
			resParts = append(resParts, m.Resolution)
		}
	}

	fields := make(map[string]plan.FieldRepresentation, len(merged.order))
	for _, name := range merged.order {
		subExpr := merged.properties[name]
		presence := plan.PresenceOptional
		if required[name] {
			presence = plan.PresenceRequired
		}
		nullable := subExpr.Kinds().Has(plan.KindNull)
		buildExpr := subExpr
		if nullable {
			if nonNull := subExpr.Kinds() &^ plan.SetNull; nonNull != 0 {
				// Strip null out of the field's own representation: nullability is
				// carried by FieldRepresentation.Nullable instead (design §7.1).
				buildExpr = ir.All{Operands: []ir.Expr{subExpr, ir.Kinds{Set: nonNull}}}
			}
		}
		sub := b.build(buildExpr, path+"/properties/"+name)
		fields[name] = plan.FieldRepresentation{
			Representation: sub.Representation,
			Presence:       presence,
			Nullable:       nullable,
		}
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	var additional plan.Representation
	if merged.additionalProperties == nil {
		// additionalProperties absent defaults to true (arbitrary extra values allowed);
		// keep the representation sound by admitting any value there (design §24).
		additional = plan.AnyRepresentation{}
	} else {
		if _, never := merged.additionalProperties.(ir.Never); never {
			additional = plan.NeverRepresentation{} // additionalProperties: false
		} else {
			sub := b.build(merged.additionalProperties, path+"/additionalProperties")
			additional = sub.Representation
			capLevel = maxCapability(capLevel, sub.Capability)
			resParts = append(resParts, sub.Resolution)
		}
	}

	var patternRules []plan.PatternFieldRepresentation
	for _, pp := range merged.patternProperties {
		sub := b.build(pp.Schema, path+"/patternProperties/"+pp.Pattern)
		patternRules = append(patternRules, plan.PatternFieldRepresentation{
			Pattern:        pp.Pattern,
			Representation: sub.Representation,
		})
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	if merged.unevaluated {
		// v1 scope (docs/implementation.md): no evaluated-annotation tracking engine.
		b.diag(path, plan.SeverityError, "unevaluatedProperties requires evaluated-property tracking")
		capLevel = maxCapability(capLevel, plan.EvaluationStateValidation)
	}

	rep := plan.ObjectRepresentation{Fields: fields, Additional: additional, PatternRules: patternRules}
	var disp plan.DispatchPlan = plan.NoDispatch{}
	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}

// mergeArrayShapes intersects every ArrayShape sibling found in an All, position-wise
// for the prefix and via conjunction for the trailing item schema (design §13).
func mergeArrayShapes(shapes []ir.ShapeDetail) (prefix []ir.Expr, items ir.Expr, unevaluated bool) {
	for _, sd := range shapes {
		as, ok := sd.(ir.ArrayShape)
		if !ok {
			continue
		}
		for i, p := range as.PrefixItems {
			if i < len(prefix) {
				prefix[i] = ir.All{Operands: []ir.Expr{prefix[i], p}}
			} else {
				prefix = append(prefix, p)
			}
		}
		if as.Items != nil {
			if items == nil {
				items = as.Items
			} else {
				items = ir.All{Operands: []ir.Expr{items, as.Items}}
			}
		}
		if as.UnevaluatedItems != nil {
			unevaluated = true
		}
	}
	return prefix, items, unevaluated
}

// buildArray infers an ArrayRepresentation (design §7, §13): a tuple prefix plus a
// homogeneous rest, defaulting the rest to Any when `items` is absent (trailing
// elements are unconstrained per spec default, so soundness requires admitting them).
func (b *builder) buildArray(c components, path string) plan.CompilationPlan {
	prefixExprs, itemsExpr, unevaluated := mergeArrayShapes(c.shapes)

	var val plan.ValidationPlan
	capLevel := plan.DirectGoType
	var resParts []plan.ResolutionPlan
	for _, p := range c.predicates {
		if p.Guard&plan.SetArray == 0 {
			continue
		}
		m := b.mapPredicate(p, path)
		if m.Expr != nil {
			val.Predicates = append(val.Predicates, plan.GuardedPredicate{Applicability: plan.SetArray, Expression: m.Expr})
		}
		capLevel = maxCapability(capLevel, m.Capability)
		if m.Resolution != nil {
			resParts = append(resParts, m.Resolution)
		}
	}

	prefix := make([]plan.Representation, len(prefixExprs))
	for i, pe := range prefixExprs {
		sub := b.build(pe, path+"/prefixItems/"+strconv.Itoa(i))
		prefix[i] = sub.Representation
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	var rest plan.Representation
	switch {
	case itemsExpr != nil:
		sub := b.build(itemsExpr, path+"/items")
		rest = sub.Representation
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	default:
		rest = plan.AnyRepresentation{}
	}

	if unevaluated {
		b.diag(path, plan.SeverityError, "unevaluatedItems requires evaluated-item tracking")
		capLevel = maxCapability(capLevel, plan.EvaluationStateValidation)
	}

	rep := plan.ArrayRepresentation{Prefix: prefix, Rest: rest}
	var disp plan.DispatchPlan = plan.NoDispatch{}
	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}
