package planner

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/plan"
)

// renormalizeBudget bounds the distribution steps spent re-normalizing an intersection
// the planner composes itself (design §21.2). Its operands are already normalized and
// only one kind restriction is pushed in, so the work is bounded by the sub-expression's
// own combinator nesting; exhausting the budget only means staying factored, which is
// what the planner did unconditionally before.
const renormalizeBudget = 64

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
// kind at runtime). Object/array shape keywords do not contribute an
// ObjectRepresentation/ArrayRepresentation either (design §12.1: `properties` alone must
// not become a struct); they survive as [plan.ShapePredicate]s guarded on their own kind,
// so the constraint is enforced for an object or array instance and vacuous for every
// other kind (see [builder.guardedShapes]).
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

	// The shape sub-plans go through buildObject/buildArray, so the v1-unsupported
	// unevaluatedProperties/unevaluatedItems flags are raised there, not here.
	shaped, shapeCap, shapeRes := b.guardedShapes(c, path)
	val.Predicates = append(val.Predicates, shaped...)
	capLevel = maxCapability(capLevel, shapeCap)
	resParts = append(resParts, shapeRes...)

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
	var p plan.CompilationPlan
	switch kind {
	case plan.KindObject:
		p = b.buildObject(c, path)
	case plan.KindArray:
		p = b.buildArray(c, path)
	default:
		p = b.buildScalar(kind, c, path)
	}
	return assertKind(pinLiteral(p, kind, c.literal), kind)
}

// assertKind states the decided kind in the validation plan, where design §4.1 says
// acceptance lives. The representation says the same thing, but as storage: a consumer
// that validates from the plan's checks alone must still reject an instance of the wrong
// kind, and before this the only thing rejecting it was the Go shape.
//
// It is prepended so the kind is checked before the predicates guarded on it, which makes
// a rejection report the type failure rather than a vacuously-passing bound.
func assertKind(p plan.CompilationPlan, kind plan.JSONKind) plan.CompilationPlan {
	assertion := plan.GuardedPredicate{Applicability: kindBit(kind), Assert: true}
	p.Validation.Predicates = append([]plan.GuardedPredicate{assertion}, p.Validation.Predicates...)
	return p
}

// pinLiteral enforces a sibling `const`/`enum` literal that survived alongside a `type`
// (design §11.3: a literal contributes both representation information and an equality
// predicate). Without it the kind-restricted representation alone would accept every
// value of that kind — an over-approximation with no residual check, which §24 forbids.
// The literal is lowered as a single-case LiteralDispatch, exactly as a bare `const` is
// (design §18 discriminator class 2).
func pinLiteral(p plan.CompilationPlan, kind plan.JSONKind, lit *ir.Literal) plan.CompilationPlan {
	if lit == nil || literalKind(lit.Value) != kind {
		return p
	}
	if _, plain := p.Dispatch.(plan.NoDispatch); !plain {
		return p
	}
	p.Dispatch = plan.LiteralDispatch{Cases: []plan.LiteralCase{{
		Value: lit.Value,
		Raw:   lit.Raw,
		Plan: plan.CompilationPlan{
			Representation: p.Representation,
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
			Capability:     plan.DirectGoType,
		},
	}}}
	p.Capability = maxCapability(p.Capability, classify(p.Representation, p.Validation, p.Dispatch, p.Resolution))
	return p
}

func (b *builder) buildScalar(kind plan.JSONKind, c components, path string) plan.CompilationPlan {
	guard := kindBit(kind)
	var val plan.ValidationPlan
	capLevel := plan.DirectGoType
	var resParts []plan.ResolutionPlan
	var formats []string
	for _, p := range c.predicates {
		if p.Guard&guard == 0 {
			continue // vacuous for this kind: the guard never fires, safe to drop.
		}
		if fd, ok := p.Detail.(ir.FormatDetail); ok {
			formats = append(formats, fd.Format)
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

	rep := plan.Representation(plan.PrimitiveRepresentation{
		Kind:    kind,
		Numeric: c.numeric,
		Format:  b.pickFormat(formats, path),
	})
	var disp plan.DispatchPlan = plan.NoDispatch{}
	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}

// buildLiteral builds the plan for a bare literal (const), lowered as a single-case
// LiteralDispatch (design §18 discriminator class 2) so the exact value is enforced by
// dispatch rather than left unchecked in an over-broad primitive representation.
//
// The literal's own kind picks the representation through the same kind-restricted
// builders a sibling `type` would go through, so an object- or array-valued literal
// yields an Object/ArrayRepresentation rather than a PrimitiveRepresentation, which
// docs/integration.md §1 reserves for Go scalars (issue #58).
func (b *builder) buildLiteral(v ir.Literal, path string) plan.CompilationPlan {
	return b.buildLeaf(literalKind(v.Value), components{numeric: plan.AnyNumber, literal: &v}, path)
}

// buildObject infers an ObjectRepresentation (design §7, §12): fields carry the
// three-state presence/nullable model (§7.1, §12.2), independent of each other.
func (b *builder) buildObject(c components, path string) plan.CompilationPlan {
	merged := b.mergeObjectShapes(c.shapes, path)
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

	fields := make([]plan.FieldRepresentation, 0, len(merged.order))
	for _, name := range merged.order {
		subExpr := merged.fields[name]
		presence := plan.PresenceOptional
		if required[name] {
			presence = plan.PresenceRequired
		}
		nullable := subExpr.Kinds().Has(plan.KindNull)
		buildExpr := subExpr
		if nullable {
			if nonNull := subExpr.Kinds() &^ plan.SetNull; nonNull != 0 {
				// Strip null out of the field's own representation: nullability is
				// carried by FieldRepresentation.Nullable instead (design §7.1). The
				// intersection is composed here rather than by norm, so it has to be
				// re-normalized: otherwise the null branch of a union survives as a
				// hollow Never alternative instead of being dropped (design §15.4-15.5,
				// issue #50).
				buildExpr = norm.Normalize(ir.All{Operands: []ir.Expr{subExpr, ir.Kinds{Set: nonNull}}}, renormalizeBudget)
			}
		}
		sub := b.build(buildExpr, pointerAppend(path+"/properties", name))
		fields = append(fields, plan.FieldRepresentation{
			Name:     name,
			Plan:     sub,
			Presence: presence,
			Nullable: nullable,
			Metadata: merged.metadata[name],
		})
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	var additional *plan.CompilationPlan
	switch {
	case merged.additional == nil:
		// additionalProperties absent defaults to true (arbitrary extra values allowed);
		// keep the representation sound by admitting any value there (design §24).
		additional = &plan.CompilationPlan{
			Representation: plan.AnyRepresentation{},
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
			Capability:     plan.DirectGoType,
		}
	case isNever(merged.additional):
		additional = &plan.CompilationPlan{ // additionalProperties: false
			Representation: plan.NeverRepresentation{},
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
			Capability:     plan.DirectGoType,
		}
	default:
		sub := b.build(merged.additional, path+"/additionalProperties")
		additional = &sub
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	var patternRules []plan.PatternFieldRepresentation
	for _, pp := range merged.patternRules {
		sub := b.build(pp.Schema, pointerAppend(path+"/patternProperties", pp.Pattern))
		patternRules = append(patternRules, plan.PatternFieldRepresentation{
			Pattern:  pp.Pattern,
			Plan:     sub,
			Metadata: pp.Metadata,
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

// buildArray infers an ArrayRepresentation (design §7, §13): a tuple prefix plus a
// homogeneous rest, defaulting the rest to Any when `items` is absent (trailing
// elements are unconstrained per spec default, so soundness requires admitting them).
func (b *builder) buildArray(c components, path string) plan.CompilationPlan {
	merged := mergeArrayShapes(c.shapes)

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

	prefix := make([]plan.ItemRepresentation, len(merged.prefix))
	for i, pe := range merged.prefix {
		sub := b.build(pe.Schema, path+"/prefixItems/"+strconv.Itoa(i))
		prefix[i] = plan.ItemRepresentation{Plan: sub, Metadata: pe.Metadata}
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}

	var rest plan.ItemRepresentation
	switch {
	case merged.items.Schema != nil:
		sub := b.build(merged.items.Schema, path+"/items")
		rest = plan.ItemRepresentation{Plan: sub, Metadata: merged.items.Metadata}
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	default:
		rest = plan.ItemRepresentation{Plan: plan.CompilationPlan{
			Representation: plan.AnyRepresentation{},
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
			Capability:     plan.DirectGoType,
		}}
	}

	if merged.unevaluated {
		b.diag(path, plan.SeverityError, "unevaluatedItems requires evaluated-item tracking")
		capLevel = maxCapability(capLevel, plan.EvaluationStateValidation)
	}

	rep := plan.ArrayRepresentation{Prefix: prefix, Rest: rest}
	var disp plan.DispatchPlan = plan.NoDispatch{}
	res := mergeResolution(resParts...)
	capLevel = maxCapability(capLevel, classify(rep, val, disp, res))
	return plan.CompilationPlan{Representation: rep, Validation: val, Dispatch: disp, Resolution: res, Capability: capLevel}
}

// isNever reports whether e is the unsatisfiable expression, which `additionalProperties:
// false` normalizes to.
func isNever(e ir.Expr) bool {
	_, never := e.(ir.Never)
	return never
}
