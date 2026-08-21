package planterp

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
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
		return accepted(), nil
	case plan.KindDispatch:
		return in.kindDispatch(d, value, f)
	case plan.LiteralDispatch:
		return in.literalDispatch(planwalk.DispatchNode(d), value, value, "literal dispatch", f)
	case plan.PropertyDispatch:
		return in.propertyDispatch(d, value, f)
	case plan.PresenceDispatch:
		return in.presenceDispatch(d, value, f)
	case plan.PredicateCountDispatch:
		return in.predicateCountDispatch(d, value, f)
	default:
		return Verdict{}, internalf("unhandled plan.DispatchPlan variant %T", d)
	}
}

func (in *interp) kindDispatch(d plan.KindDispatch, value any, f frame) (Verdict, error) {
	k, err := kindOf(value)
	if err != nil {
		return Verdict{}, withPath(f.path, err)
	}

	for c := range planwalk.Children(planwalk.DispatchNode(d)) {
		if c.Edge.Kind != planwalk.EdgeKindCase {
			return Verdict{}, internalf("unhandled kind dispatch child edge %s", c.Edge.Kind)
		}
		if c.Edge.Case != k {
			continue
		}
		v, err := in.plan(c.Plan, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, "kind dispatch", kindName(k)+" case rejects", v.Reason), nil
		}
		return accepted(), nil
	}
	return rejected(f, "kind dispatch", "no case for "+kindName(k)), nil
}

// literalDispatch selects the case whose literal equals selector and runs its plan
// against value. For a [plan.LiteralDispatch] the two are the same value; for a
// [plan.PropertyDispatch] the selector is the tag property's value. Both dispatches
// enumerate their cases the same way, as literal-labeled children of n.
func (in *interp) literalDispatch(n planwalk.Node, selector, value any, what string, f frame) (Verdict, error) {
	for c := range planwalk.Children(n) {
		switch c.Edge.Kind {
		case planwalk.EdgeLiteralCase, planwalk.EdgePropertyCase:
		default:
			return Verdict{}, internalf("unhandled literal dispatch child edge %s", c.Edge.Kind)
		}

		eq, err := equalValues(selector, edgeLiteral(c.Edge))
		if err != nil {
			return Verdict{}, withPath(f.path, err)
		}
		if !eq {
			continue
		}
		v, err := in.plan(c.Plan, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, what, "selected case rejects", v.Reason), nil
		}
		return accepted(), nil
	}
	return rejected(f, what, "no case matches the value"), nil
}

func (in *interp) propertyDispatch(d plan.PropertyDispatch, value any, f frame) (Verdict, error) {
	switch d.Tag {
	case plan.TagInferred, plan.TagDeclared, plan.TagAsserted:
	default:
		return Verdict{}, internalf("unhandled plan.TagSource %d", d.Tag)
	}

	what := "property dispatch[" + strconv.Quote(d.Property) + "]"
	obj, ok := value.(map[string]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, withPath(f.path, err)
		}
		return rejected(f, what, "value is "+kindName(k)), nil
	}
	sel, present := obj[d.Property]
	if !present {
		return rejected(f, what, "tag is absent"), nil
	}
	return in.literalDispatch(planwalk.DispatchNode(d), sel, value, what, f)
}

func (in *interp) presenceDispatch(d plan.PresenceDispatch, value any, f frame) (Verdict, error) {
	var present, absent plan.CompilationPlan
	for c := range planwalk.Children(planwalk.DispatchNode(d)) {
		switch c.Edge.Kind {
		case planwalk.EdgePresent:
			present = c.Plan
		case planwalk.EdgeAbsent:
			absent = c.Plan
		default:
			return Verdict{}, internalf("unhandled presence dispatch child edge %s", c.Edge.Kind)
		}
	}

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
	if _, ok := obj[d.Property]; ok {
		branch, label = present, "present"
	}
	v, err := in.plan(branch, value, f)
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted {
		return rejectedBy(f, "presence dispatch["+strconv.Quote(d.Property)+" "+label+"]", "", v.Reason), nil
	}
	return accepted(), nil
}

// predicateCountDispatch is the runtime match-count of docs/integration.md §3: every
// branch is trial-validated and the number of accepting branches must fall in range.
func (in *interp) predicateCountDispatch(d plan.PredicateCountDispatch, value any, f frame) (Verdict, error) {
	matches := 0
	var last *ValidateError
	for c := range planwalk.Children(planwalk.DispatchNode(d)) {
		if c.Edge.Kind != planwalk.EdgeCountBranch {
			return Verdict{}, internalf("unhandled predicate-count dispatch child edge %s", c.Edge.Kind)
		}
		v, err := in.plan(c.Plan, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if v.Accepted {
			matches++
			continue
		}
		last = v.Reason
	}
	if matches < d.Minimum || matches > d.Maximum {
		return rejectedBy(f, "predicate-count dispatch",
			strconv.Itoa(matches)+" branches match, want "+
				strconv.Itoa(d.Minimum)+"-"+strconv.Itoa(d.Maximum), last), nil
	}
	return accepted(), nil
}
