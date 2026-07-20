// Package planner turns a normalized [ir.Expr] into an analyzed [plan.CompilationPlan]
// (design §21, §22): it infers a Go [plan.Representation], extracts residual
// [plan.ValidationPlan] predicates, builds a [plan.DispatchPlan] for combinators, a
// [plan.ResolutionPlan] for references, and classifies the result into the
// [plan.CapabilityLevel] ladder while enforcing the v1 scope (docs/implementation.md).
//
// The planner assumes its input Expr is already normalized (phase 3, internal/norm):
// it does not itself prove disjointness or push constraints, though it defensively
// intersects sibling constraints into combinator branches so a not-yet-fully-normalized
// input still produces a sound (if less precise) plan.
package planner

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// Result is the planner's output for one schema: the analyzed plan plus the
// diagnostics collected while building it and the top-level exactness (design §25).
type Result struct {
	Plan        plan.CompilationPlan
	Diagnostics []plan.Diagnostic
	Exactness   plan.Exactness
}

// Build converts a normalized ir.Expr into a plan.CompilationPlan (design §21). reg may
// be nil when the expression carries no references (e.g. hand-built test fixtures);
// Build only consults it for recursion classification (design §19) and dynamic-ref
// presence (design §10.2).
func Build(e ir.Expr, reg *frontend.Registry) Result {
	b := newBuilder(reg)
	p := b.build(e, "")
	return Result{
		Plan:        p,
		Diagnostics: b.diags,
		Exactness:   exactnessOf(p),
	}
}

// builder carries the shared, read-only recursion index and accumulates diagnostics
// across one Build call (including into nested dispatch/resolution branches).
type builder struct {
	reg      *frontend.Registry
	recur    map[plan.SchemaID]frontend.RecursionClass
	refCache map[string]*frontend.Node
	diags    []plan.Diagnostic
}

// refTargets returns the registry's static-$ref target index, built once per Build call.
// Returns nil when reg is nil (hand-built fixtures), in which case refs are not followed.
func (b *builder) refTargets() map[string]*frontend.Node {
	if b.reg == nil {
		return nil
	}
	if b.refCache == nil {
		b.refCache = b.reg.RefTargets()
	}
	return b.refCache
}

func newBuilder(reg *frontend.Registry) *builder {
	b := &builder{reg: reg}
	if reg != nil {
		b.recur = make(map[plan.SchemaID]frontend.RecursionClass)
		for _, scc := range reg.SCCs() {
			for _, n := range scc.Nodes {
				b.recur[plan.SchemaID(n.Pointer)] = scc.Class
			}
		}
	}
	return b
}

// diag records a diagnostic (design §25).
func (b *builder) diag(pointer string, sev plan.Severity, msg string) {
	b.diags = append(b.diags, plan.Diagnostic{Pointer: pointer, Severity: sev, Message: msg})
}

// build is the main recursive entry point (design §21): it dispatches on the concrete
// ir.Expr variant, producing one CompilationPlan per node. path is a best-effort,
// human-readable breadcrumb (not a source-exact JSON Pointer, since ir.Expr does not
// retain one) used only for diagnostics.
func (b *builder) build(e ir.Expr, path string) plan.CompilationPlan {
	switch v := e.(type) {
	case ir.Any:
		return anyPlan()
	case ir.Never:
		return b.neverPlanAt(path)
	case ir.Kinds, ir.Predicate, ir.Shape, ir.Not:
		return b.buildAll(ir.All{Operands: []ir.Expr{e}}, path)
	case ir.Literal:
		return b.buildLiteral(v, path)
	case ir.All:
		return b.buildAll(v, path)
	case ir.AnyOf:
		return b.buildUnionWithContext(e.Kinds(), e, components{}, path)
	case ir.ExactlyOne:
		return b.buildUnionWithContext(e.Kinds(), e, components{}, path)
	case ir.Ref:
		return b.buildRef(v, path)
	case ir.DynamicRef:
		return b.buildDynamicRef(v, path)
	case ir.Annotated:
		// Evaluation annotations proper are not yet modeled (ir.EvaluationAnnotations is
		// still an empty marker); unevaluatedProperties/unevaluatedItems are detected via
		// ObjectShape/ArrayShape fields instead (see resolution.go/classify.go).
		return b.build(v.Expr, path)
	default:
		// Unrecognized node: widen soundly rather than guess (design §24 invariant 4).
		b.diag(path, plan.SeverityWarning, "unrecognized expression node, widened to any")
		return anyPlan()
	}
}

// components is the flattened, structural set of sibling contributions found in an
// ir.All (or a single bare node treated as a one-element All). Kind information is not
// duplicated here: the caller uses the original expression's Kinds() (already computed
// correctly by ir, design §6) for the combined kind restriction.
type components struct {
	shapes      []ir.ShapeDetail
	predicates  []ir.Predicate
	literal     *ir.Literal
	combinators []ir.Expr // AnyOf / ExactlyOne
	nots        []ir.Not
	refs        []ir.Expr // Ref / DynamicRef
	numeric     plan.NumericDomain
	never       bool
	// hasKindRestriction reports whether an explicit ir.Kinds sibling was present. When
	// false, the combined kind set being SetAny reflects the absence of a `type`
	// keyword (design §3), not an explicit type array listing every kind: the two must
	// be told apart so a bare predicate/shape widens to Any instead of fanning out into
	// a per-kind KindDispatch.
	hasKindRestriction bool
}

// flattenAll recursively splits nested ir.All/ir.Annotated operands into their leaf
// contributions (design §15.1's associative flattening, applied defensively here).
func flattenAll(operands []ir.Expr) components {
	c := components{numeric: plan.AnyNumber}
	var walk func(e ir.Expr)
	walk = func(e ir.Expr) {
		switch v := e.(type) {
		case ir.Any:
			// Contributes nothing.
		case ir.Kinds:
			// The kind bitmask is already folded into the caller's Kinds() aggregate;
			// only the numeric refinement needs to be tracked separately, since All's
			// Kinds() does not propagate it.
			c.hasKindRestriction = true
			if v.Numeric != plan.AnyNumber {
				if c.numeric == plan.AnyNumber {
					c.numeric = v.Numeric
				} else if c.numeric != v.Numeric {
					c.never = true
				}
			}
		case ir.Never:
			c.never = true
		case ir.All:
			for _, o := range v.Operands {
				walk(o)
			}
		case ir.Annotated:
			walk(v.Expr)
		case ir.Literal:
			lit := v
			c.literal = &lit
		case ir.Predicate:
			c.predicates = append(c.predicates, v)
		case ir.Shape:
			c.shapes = append(c.shapes, v.Detail)
		case ir.AnyOf:
			c.combinators = append(c.combinators, v)
		case ir.ExactlyOne:
			c.combinators = append(c.combinators, v)
		case ir.Not:
			c.nots = append(c.nots, v)
		case ir.Ref:
			c.refs = append(c.refs, v)
		case ir.DynamicRef:
			c.refs = append(c.refs, v)
		}
	}
	for _, o := range operands {
		walk(o)
	}
	return c
}
