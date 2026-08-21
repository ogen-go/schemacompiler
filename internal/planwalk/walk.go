// Package planwalk is the single structural traversal of the [plan] types (issue #47).
//
// Every case destructures its value into an anonymous struct and traverses THAT binding
// rather than the original. Go assignability requires identical underlying types, so
// adding, renaming, retyping or reordering a field of a plan struct stops this package
// compiling, and the traversal cannot be repaired without naming the new field. The
// binding form is deliberate: a discard guard would compile identically while letting a
// new field be silenced without ever being walked.
//
// Interface variants are not covered by that mechanism, since Go has no sealed-interface
// exhaustiveness check. Each switch therefore ends in a panicking default, and
// [AllRepresentations], [AllDispatchPlans] and [AllPredicateExprs] give tests a list to
// drive every variant through it.
package planwalk

import (
	"fmt"

	"github.com/ogen-go/schemacompiler/plan"
)

// Plan calls visit for every [plan.Representation] reachable from p: those of its own
// representation tree, and those of the plans nested in its dispatch branches and in the
// residual predicates that carry a plan.
//
// p.Resolution is bound but not descended: its definitions are whole-document plans that
// callers hold separately, and walking them here would double-visit them.
func Plan(p plan.CompilationPlan, visit func(plan.Representation)) {
	var t struct {
		Representation plan.Representation
		Validation     plan.ValidationPlan
		Dispatch       plan.DispatchPlan
		Resolution     plan.ResolutionPlan
		Capability     plan.CapabilityLevel
		Metadata       plan.Metadata
	} = p

	Representation(t.Representation, visit)
	Dispatch(t.Dispatch, func(child plan.CompilationPlan) { Plan(child, visit) })
	Validation(t.Validation, func(child plan.CompilationPlan) { Plan(child, visit) })
}

// Representation calls visit for r and for every representation nested within it,
// in pre-order. A nil r visits nothing.
func Representation(r plan.Representation, visit func(plan.Representation)) {
	if r == nil {
		return
	}
	visit(r)

	switch r := r.(type) {
	case plan.AnyRepresentation:
		var t struct{} = r
		_ = t
	case plan.NeverRepresentation:
		var t struct{} = r
		_ = t
	case plan.PrimitiveRepresentation:
		var t struct {
			Kind    plan.JSONKind
			Numeric plan.NumericDomain
			Format  string
		} = r
		_ = t
	case plan.ObjectRepresentation:
		var t struct {
			Fields       map[string]plan.FieldRepresentation
			Additional   plan.Representation
			PatternRules []plan.PatternFieldRepresentation
		} = r
		for _, f := range t.Fields {
			field(f, visit)
		}
		Representation(t.Additional, visit)
		for _, pr := range t.PatternRules {
			patternField(pr, visit)
		}
	case plan.ArrayRepresentation:
		var t struct {
			Prefix []plan.ItemRepresentation
			Rest   plan.ItemRepresentation
		} = r
		for _, it := range t.Prefix {
			item(it, visit)
		}
		item(t.Rest, visit)
	case plan.UnionRepresentation:
		var t struct {
			Alternatives []plan.Representation
		} = r
		for _, alt := range t.Alternatives {
			Representation(alt, visit)
		}
	case plan.RecursiveRepresentation:
		var t struct {
			Name string
			Body plan.Representation
		} = r
		Representation(t.Body, visit)
	case plan.ReferenceRepresentation:
		var t struct {
			Name string
		} = r
		_ = t
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.Representation variant %T", r))
	}
}

func field(f plan.FieldRepresentation, visit func(plan.Representation)) {
	var t struct {
		Representation plan.Representation
		Presence       plan.PresenceMode
		Nullable       bool
		Metadata       plan.Metadata
	} = f
	Representation(t.Representation, visit)
}

func patternField(p plan.PatternFieldRepresentation, visit func(plan.Representation)) {
	var t struct {
		Pattern        string
		Representation plan.Representation
		Metadata       plan.Metadata
	} = p
	Representation(t.Representation, visit)
}

func item(i plan.ItemRepresentation, visit func(plan.Representation)) {
	var t struct {
		Representation plan.Representation
		Metadata       plan.Metadata
	} = i
	Representation(t.Representation, visit)
}

// Dispatch calls visit for every [plan.CompilationPlan] d selects between. A nil d
// visits nothing.
func Dispatch(d plan.DispatchPlan, visit func(plan.CompilationPlan)) {
	if d == nil {
		return
	}

	switch d := d.(type) {
	case plan.NoDispatch:
		var t struct{} = d
		_ = t
	case plan.KindDispatch:
		var t struct {
			Cases map[plan.JSONKind]plan.CompilationPlan
		} = d
		for _, branch := range t.Cases {
			visit(branch)
		}
	case plan.LiteralDispatch:
		var t struct {
			Cases []plan.LiteralCase
		} = d
		literalCases(t.Cases, visit)
	case plan.PropertyDispatch:
		var t struct {
			Property string
			Cases    []plan.LiteralCase
			Tag      plan.TagSource
		} = d
		literalCases(t.Cases, visit)
	case plan.PresenceDispatch:
		var t struct {
			Property string
			Present  plan.CompilationPlan
			Absent   plan.CompilationPlan
		} = d
		visit(t.Present)
		visit(t.Absent)
	case plan.PredicateCountDispatch:
		var t struct {
			Branches []plan.CompilationPlan
			Minimum  int
			Maximum  int
		} = d
		for _, branch := range t.Branches {
			visit(branch)
		}
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.DispatchPlan variant %T", d))
	}
}

func literalCases(cases []plan.LiteralCase, visit func(plan.CompilationPlan)) {
	for _, c := range cases {
		var t struct {
			Value any
			Raw   []byte
			Plan  plan.CompilationPlan
		} = c
		visit(t.Plan)
	}
}

// Validation calls visit for every [plan.CompilationPlan] carried by a residual
// predicate of v.
func Validation(v plan.ValidationPlan, visit func(plan.CompilationPlan)) {
	var t struct {
		Predicates []plan.GuardedPredicate
	} = v
	for _, gp := range t.Predicates {
		var g struct {
			Applicability plan.KindSet
			Expression    plan.PredicateExpr
		} = gp
		Predicate(g.Expression, visit)
	}
}

// Predicate calls visit for every [plan.CompilationPlan] e carries. Most variants carry
// none. A nil e visits nothing.
func Predicate(e plan.PredicateExpr, visit func(plan.CompilationPlan)) {
	if e == nil {
		return
	}

	switch e := e.(type) {
	case plan.MinLengthPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxLengthPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.PatternPredicate:
		var t struct{ Regex string } = e
		_ = t
	case plan.FormatPredicate:
		var t struct{ Format string } = e
		_ = t
	case plan.MinimumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		_ = t
	case plan.MaximumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		_ = t
	case plan.MultipleOfPredicate:
		var t struct{ Value float64 } = e
		_ = t
	case plan.MinItemsPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxItemsPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.UniqueItemsPredicate:
		var t struct{} = e
		_ = t
	case plan.ContainsCountPredicate:
		var t struct {
			Schema plan.CompilationPlan
			Min    uint64
			Max    *uint64
		} = e
		visit(t.Schema)
	case plan.RequiredPredicate:
		var t struct{ Properties []string } = e
		_ = t
	case plan.MinPropertiesPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxPropertiesPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.DependentRequiredPredicate:
		var t struct{ Entries []plan.DependentRequiredEntry } = e
		_ = t
	case plan.PropertyNamesPredicate:
		var t struct{ Schema plan.CompilationPlan } = e
		visit(t.Schema)
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.PredicateExpr variant %T", e))
	}
}
