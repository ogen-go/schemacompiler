package nodetree

import (
	"math/big"
	"strconv"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// ErrUnsupported is returned for a construct this spike does not lower. It is an error
// rather than a skipped check: a validator that ignores a constraint accepts what the
// schema rejects (design §24).
var ErrUnsupported = errors.New("nodetree: unsupported construct")

//nolint:gocyclo // one case per plan.PredicateExpr variant, as planterp's switch is.
func (v *Validator) compileExpr(e plan.PredicateExpr, fusion planFusion) (node, error) {
	switch e := e.(type) {
	case nil:
		return nil, nil

	case plan.MinLengthPredicate:
		return lengthBound{bound: e.Value, isMin: true}, nil
	case plan.MaxLengthPredicate:
		return lengthBound{bound: e.Value}, nil
	case plan.PatternPredicate:
		return patternNode{regex: e.Regex}, nil

	case plan.FormatPredicate:
		// `format` is an annotation in the 2020-12 standard dialect; assertion requires
		// opting into format-assertion, which no plan carries (docs/integration.md §1.1).
		return nil, nil

	case plan.MinimumPredicate:
		n, ok := newNumericBound(e.Value, e.Exclusive, true)
		if !ok {
			return nil, errors.Wrapf(ErrUnsupported, "minimum %v", e.Value)
		}
		return n, nil
	case plan.MaximumPredicate:
		n, ok := newNumericBound(e.Value, e.Exclusive, false)
		if !ok {
			return nil, errors.Wrapf(ErrUnsupported, "maximum %v", e.Value)
		}
		return n, nil
	case plan.MultipleOfPredicate:
		div := new(big.Rat)
		if _, ok := div.SetString(strconv.FormatFloat(e.Value, 'g', -1, 64)); !ok || div.Sign() == 0 {
			return nil, errors.Wrapf(ErrUnsupported, "multipleOf %v", e.Value)
		}
		return multipleOf{divisor: div}, nil
	case plan.NumericDomainPredicate:
		return numericDomain{domain: e.Domain}, nil

	case plan.MinItemsPredicate:
		return itemsBound{bound: e.Value, isMin: true}, nil
	case plan.MaxItemsPredicate:
		return itemsBound{bound: e.Value}, nil
	case plan.UniqueItemsPredicate:
		return uniqueItems{}, nil

	case plan.RequiredPredicate:
		set := newNameSet(e.Properties)
		return requiredNode{set: set, wanted: set.maskOf(e.Properties)}, nil
	case plan.MinPropertiesPredicate:
		return propertiesBound{bound: e.Value, isMin: true}, nil
	case plan.MaxPropertiesPredicate:
		return propertiesBound{bound: e.Value}, nil
	case plan.DependentRequiredPredicate:
		return newDependentRequired(e.Entries), nil

	case plan.ObjectStructurePredicate:
		return v.compileObjectStructure(e, fusion.objectBounds)
	case plan.ArrayStructurePredicate:
		return v.compileArrayStructure(e, fusion.arrayBounds)

	case plan.ReferencePredicate:
		return reference{name: e.Name, defs: v.defs}, nil

	case plan.NegationPredicate:
		inner, err := v.compilePlan(e.Schema)
		if err != nil {
			return nil, err
		}
		return negation{inner: inner}, nil
	case plan.ShapePredicate:
		inner, err := v.compilePlan(e.Schema)
		if err != nil {
			return nil, err
		}
		return shape{inner: inner}, nil
	case plan.PropertyNamesPredicate:
		inner, err := v.compilePlan(e.Schema)
		if err != nil {
			return nil, err
		}
		return propertyNames{inner: inner}, nil
	case plan.ContainsCountPredicate:
		inner, err := v.compilePlan(e.Schema)
		if err != nil {
			return nil, err
		}
		c := containsCount{inner: inner, min: e.Min}
		if e.Max != nil {
			c.max, c.hasMax = *e.Max, true
		}
		return c, nil

	default:
		return nil, errors.Wrapf(ErrUnsupported, "predicate %T", e)
	}
}

func (v *Validator) compileObjectStructure(e plan.ObjectStructurePredicate, counts countBounds) (node, error) {
	declared := make([]string, len(e.Properties))
	for i, pc := range e.Properties {
		declared[i] = pc.Name
	}
	o := objectStructure{set: newNameSet(declared), counts: counts}
	o.required = o.set.newPresence()
	for i, pc := range e.Properties {
		if pc.Presence == plan.PresenceRequired {
			o.required.set(i)
		}
		n, err := v.compilePlan(pc.Plan)
		if err != nil {
			return nil, err
		}
		o.fields = append(o.fields, fieldCheck{
			name:     pc.Name,
			inner:    n,
			required: pc.Presence == plan.PresenceRequired,
			nullable: pc.Nullable,
		})
	}
	for _, pc := range e.Patterns {
		n, err := v.compilePlan(pc.Plan)
		if err != nil {
			return nil, err
		}
		o.patterns = append(o.patterns, patternField{pattern: pc.Pattern, inner: n})
	}
	if e.Additional != nil {
		n, err := v.compilePlan(*e.Additional)
		if err != nil {
			return nil, err
		}
		o.additional = n
	}
	return o, nil
}

func (v *Validator) compileArrayStructure(e plan.ArrayStructurePredicate, counts countBounds) (node, error) {
	a := arrayStructure{counts: counts}
	for _, p := range e.Prefix {
		n, err := v.compilePlan(p)
		if err != nil {
			return nil, err
		}
		a.prefix = append(a.prefix, n)
	}
	if e.Rest != nil {
		n, err := v.compilePlan(*e.Rest)
		if err != nil {
			return nil, err
		}
		a.rest = n
	} else {
		a.restForbidden = true
	}
	return a, nil
}

func (v *Validator) compileDispatch(d plan.DispatchPlan) (node, error) {
	switch d := d.(type) {
	case nil, plan.NoDispatch:
		return nil, nil

	case plan.KindDispatch:
		kd := kindDispatch{cases: map[plan.JSONKind]node{}}
		for k, c := range d.Cases {
			n, err := v.compilePlan(c)
			if err != nil {
				return nil, err
			}
			kd.cases[k] = n
		}
		return kd, nil

	case plan.LiteralDispatch:
		cases, err := v.compileCases(d.Cases)
		if err != nil {
			return nil, err
		}
		return literalDispatch{caseTable: newCaseTable(cases)}, nil

	case plan.PropertyDispatch:
		cases, err := v.compileCases(d.Cases)
		if err != nil {
			return nil, err
		}
		return propertyDispatch{property: d.Property, caseTable: newCaseTable(cases)}, nil

	case plan.PresenceDispatch:
		present, err := v.compilePlan(d.Present)
		if err != nil {
			return nil, err
		}
		absent, err := v.compilePlan(d.Absent)
		if err != nil {
			return nil, err
		}
		return presenceDispatch{property: d.Property, present: present, absent: absent}, nil

	case plan.PredicateCountDispatch:
		pc := predicateCountDispatch{min: d.Minimum, max: d.Maximum}
		for _, b := range d.Branches {
			n, err := v.compilePlan(b)
			if err != nil {
				return nil, err
			}
			pc.branches = append(pc.branches, n)
		}
		return pc, nil

	default:
		return nil, errors.Wrapf(ErrUnsupported, "dispatch %T", d)
	}
}

func (v *Validator) compileCases(cs []plan.LiteralCase) ([]literalCase, error) {
	out := make([]literalCase, 0, len(cs))
	for _, c := range cs {
		n, err := v.compilePlan(c.Plan)
		if err != nil {
			return nil, err
		}
		raw := c.Raw
		if raw == nil {
			encoded, err := encodeLiteral(c.Value)
			if err != nil {
				return nil, err
			}
			raw = encoded
		}
		out = append(out, literalCase{raw: raw, inner: n})
	}
	return out, nil
}
