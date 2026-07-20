// Package norm normalizes the semantic IR ([ir.Expr]) toward a simplified,
// analyzable form while preserving exact JSON Schema semantics (design §15).
//
// Every rewrite here must be sound: it may never change the set of accepted
// JSON values. When a proof is uncertain, a rule declines and leaves the
// expression untouched rather than approximating (design §15.2, §15.3 list
// which proofs are tractable for v1; harder ones like regex-language
// inclusion or full interval algebra are intentionally left unproved).
//
// norm imports only ir and plan; it must not import frontend or libopenapi.
package norm

import (
	"reflect"

	"github.com/ogen-go/schemacompiler/internal/ir"
)

// maxFixpointIters bounds the outer fixpoint loop as a hard safety net. Every
// individual rule either shrinks the expression or is paid for out of budget,
// so this is normally never reached; it exists so a missed edge case fails
// safe (stops and returns) instead of looping forever.
const maxFixpointIters = 64

// Normalize rewrites e to a fixpoint of the rules in this package, or until
// budget branch-distribution steps have been spent — whichever comes first
// (design §21.2). budget is the only thing that can make Normalize stop
// short of a full fixpoint: distribution (design §15.4-15.5, §17.3-17.4) is
// the sole rule that can duplicate sub-expressions, so it is the only one
// metered. Every other rule strictly shrinks the expression and needs no
// metering.
//
// Normalize is a pure function: it returns a new [ir.Expr] and never
// mutates its input, and it never errors — an unproved rewrite is simply
// skipped, never attempted unsoundly.
func Normalize(e ir.Expr, budget int) ir.Expr {
	st := &state{budget: budget}
	prev := e
	for i := 0; i < maxFixpointIters; i++ {
		next := normalize(prev, st)
		if exprEqual(next, prev) {
			return next
		}
		prev = next
	}
	return prev
}

// state threads the remaining expansion budget through a single normalize pass.
type state struct{ budget int }

// spend reports whether an expansion step may proceed, consuming one unit of
// budget if so.
func (s *state) spend() bool {
	if s.budget <= 0 {
		return false
	}
	s.budget--
	return true
}

// normalize dispatches one rewrite pass over e, recursing into children first
// (design §21.2: "flatten... simplify... push... prove..." all operate on
// already-normalized sub-expressions).
func normalize(e ir.Expr, st *state) ir.Expr {
	switch e := e.(type) {
	case ir.All:
		return normalizeAll(e, st)
	case ir.AnyOf:
		return normalizeAnyOf(e, st)
	case ir.ExactlyOne:
		return normalizeExactlyOne(e, st)
	case ir.Not:
		return normalizeNot(normalize(e.Operand, st))
	case ir.Annotated:
		return ir.Annotated{Expr: normalize(e.Expr, st), Annotations: e.Annotations}
	case ir.Shape:
		return normalizeShape(e, st)
	case ir.Predicate:
		return normalizePredicate(e, st)
	default:
		// Any, Never, Kinds, Literal, Ref, DynamicRef carry no sub-expressions.
		return e
	}
}

// normalizeNot applies the cheap, always-sound complement identities; full
// complement elimination for arbitrary operands is not attempted (design
// §11.8: it "remains a residual predicate" otherwise).
func normalizeNot(operand ir.Expr) ir.Expr {
	switch o := operand.(type) {
	case ir.Any:
		return ir.Never{}
	case ir.Never:
		return ir.Any{}
	case ir.Not:
		return o.Operand
	default:
		return ir.Not{Operand: operand}
	}
}

// normalizeShape recurses into every sub-expression a Shape carries so nested
// property/item/pattern schemas are normalized too (design §12, §13).
func normalizeShape(e ir.Shape, st *state) ir.Expr {
	switch d := e.Detail.(type) {
	case ir.ObjectShape:
		nd := ir.ObjectShape{}
		for _, p := range d.Properties {
			nd.Properties = append(nd.Properties, ir.PropertyExpr{Name: p.Name, Schema: normalize(p.Schema, st)})
		}
		for _, p := range d.PatternProperties {
			nd.PatternProperties = append(nd.PatternProperties, ir.PatternPropertyExpr{Pattern: p.Pattern, Schema: normalize(p.Schema, st)})
		}
		if d.AdditionalProperties != nil {
			nd.AdditionalProperties = normalize(d.AdditionalProperties, st)
		}
		if d.UnevaluatedProperties != nil {
			nd.UnevaluatedProperties = normalize(d.UnevaluatedProperties, st)
		}
		return ir.Shape{Detail: nd}
	case ir.ArrayShape:
		nd := ir.ArrayShape{}
		for _, it := range d.PrefixItems {
			nd.PrefixItems = append(nd.PrefixItems, normalize(it, st))
		}
		if d.Items != nil {
			nd.Items = normalize(d.Items, st)
		}
		if d.UnevaluatedItems != nil {
			nd.UnevaluatedItems = normalize(d.UnevaluatedItems, st)
		}
		return ir.Shape{Detail: nd}
	default:
		return e
	}
}

// normalizePredicate recurses into the sub-schemas a predicate detail carries
// (contains, propertyNames); the rest are leaf scalars with nothing to
// normalize.
func normalizePredicate(e ir.Predicate, st *state) ir.Expr {
	switch d := e.Detail.(type) {
	case ir.ContainsDetail:
		nd := d
		nd.Schema = normalize(d.Schema, st)
		return ir.Predicate{Guard: e.Guard, Detail: nd}
	case ir.PropertyNamesDetail:
		nd := d
		nd.Schema = normalize(d.Schema, st)
		return ir.Predicate{Guard: e.Guard, Detail: nd}
	default:
		return e
	}
}

// exprEqual is exact structural (syntactic) equality over the finite Expr
// tree — sufficient for dedup/idempotence/fixpoint checks since Expr never
// contains cycles (recursive schemas go through Ref, not an inline cycle).
func exprEqual(a, b ir.Expr) bool {
	return reflect.DeepEqual(a, b)
}
