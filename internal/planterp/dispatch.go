package planterp

import (
	"strconv"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// dispatch runs the branch selection a plan describes (design §9). Dispatch happens at
// the same instance node, so the frame is not descended.
func (in *interp) dispatch(d plan.DispatchPlan, value any, f frame) (Verdict, error) {
	if d == nil {
		return accepted(), nil
	}

	switch d := d.(type) {
	case plan.NoDispatch:
		var t struct{} = d
		_ = t
		return accepted(), nil

	case plan.KindDispatch:
		var t struct {
			Cases map[plan.JSONKind]plan.CompilationPlan
		} = d
		return in.kindDispatch(t.Cases, value, f)

	case plan.LiteralDispatch:
		var t struct {
			Cases []plan.LiteralCase
		} = d
		return in.literalDispatch(t.Cases, value, value, "literal", f)

	case plan.PropertyDispatch:
		var t struct {
			Property string
			Cases    []plan.LiteralCase
			Tag      plan.TagSource
		} = d
		return in.propertyDispatch(t.Property, t.Cases, t.Tag, value, f)

	case plan.PresenceDispatch:
		var t struct {
			Property string
			Present  plan.CompilationPlan
			Absent   plan.CompilationPlan
		} = d
		return in.presenceDispatch(t.Property, t.Present, t.Absent, value, f)

	case plan.PredicateCountDispatch:
		var t struct {
			Branches []plan.CompilationPlan
			Minimum  int
			Maximum  int
		} = d
		return in.predicateCountDispatch(t.Branches, t.Minimum, t.Maximum, value, f)

	default:
		return Verdict{}, errors.Errorf("planterp: unhandled plan.DispatchPlan variant %T", d)
	}
}

func (in *interp) kindDispatch(cases map[plan.JSONKind]plan.CompilationPlan, value any, f frame) (Verdict, error) {
	k, err := kindOf(value)
	if err != nil {
		return Verdict{}, err
	}
	branch, ok := cases[k]
	if !ok {
		return rejected("kind dispatch: no case for " + kindName(k)), nil
	}
	v, err := in.plan(branch, value, f)
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted {
		return rejected("kind dispatch[" + kindName(k) + "]: " + v.Reason), nil
	}
	return accepted(), nil
}

// literalDispatch selects the case whose literal equals selector and runs its plan
// against value. For a [plan.LiteralDispatch] the two are the same value; for a
// [plan.PropertyDispatch] the selector is the tag property's value.
func (in *interp) literalDispatch(
	cases []plan.LiteralCase,
	selector, value any,
	what string,
	f frame,
) (Verdict, error) {
	for _, c := range cases {
		eq, err := equalValues(selector, literalValue(c))
		if err != nil {
			return Verdict{}, err
		}
		if !eq {
			continue
		}
		v, err := in.plan(c.Plan, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejected(what + " dispatch: selected case rejects: " + v.Reason), nil
		}
		return accepted(), nil
	}
	return rejected(what + " dispatch: no case matches the value"), nil
}

func (in *interp) propertyDispatch(
	property string,
	cases []plan.LiteralCase,
	tag plan.TagSource,
	value any,
	f frame,
) (Verdict, error) {
	switch tag {
	case plan.TagInferred, plan.TagDeclared, plan.TagAsserted:
	default:
		return Verdict{}, errors.Errorf("planterp: unhandled plan.TagSource %d", tag)
	}

	obj, ok := value.(map[string]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, err
		}
		return rejected("property dispatch: value is " + kindName(k)), nil
	}
	sel, present := obj[property]
	if !present {
		return rejected("property dispatch: tag " + strconv.Quote(property) + " is absent"), nil
	}
	return in.literalDispatch(cases, sel, value, "property "+strconv.Quote(property), f)
}

func (in *interp) presenceDispatch(property string, present, absent plan.CompilationPlan, value any, f frame) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		// A presence dispatch comes from `dependentSchemas`, which applies to objects
		// only (design §12.7), but the plan carries no kind guard on a DispatchPlan, so
		// there is nothing here that says what a non-object should do. Accepting is the
		// sound reading: a dispatch that cannot select cannot reject.
		in.approximate("presence dispatch on a non-object")
		return accepted(), nil
	}

	branch, label := absent, "absent"
	if _, ok := obj[property]; ok {
		branch, label = present, "present"
	}
	v, err := in.plan(branch, value, f)
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted {
		return rejected("presence dispatch[" + strconv.Quote(property) + " " + label + "]: " + v.Reason), nil
	}
	return accepted(), nil
}

// predicateCountDispatch is the runtime match-count of docs/integration.md §3: every
// branch is trial-validated and the number of accepting branches must fall in range.
func (in *interp) predicateCountDispatch(branches []plan.CompilationPlan, minimum, maximum int, value any, f frame) (Verdict, error) {
	matches := 0
	for _, branch := range branches {
		v, err := in.plan(branch, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if v.Accepted {
			matches++
		}
	}
	if matches < minimum || matches > maximum {
		return rejected("predicate-count dispatch: " + strconv.Itoa(matches) + " branches match, want " +
			strconv.Itoa(minimum) + "-" + strconv.Itoa(maximum)), nil
	}
	return accepted(), nil
}
