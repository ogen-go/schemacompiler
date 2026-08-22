package planterp

import (
	"math/big"
	"strconv"
	"unicode/utf8"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// validation runs the kind-scoped checks (design §8). A guard whose Applicability does
// not include the instance's kind passes vacuously; an assertion rejects it instead
// (design §3.1), and an assertion with no Expression is the kind check itself.
func (in *interp) validation(vp plan.ValidationPlan, value any, f frame) (Verdict, error) {
	kind, err := kindOf(value)
	if err != nil {
		return Verdict{}, withPath(f.path, err)
	}

	for c := range planwalk.Children(planwalk.ValidationNode(vp)) {
		if c.Edge.Kind != planwalk.EdgeGuardedPredicate {
			return Verdict{}, internalf("unhandled validation child edge %s", c.Edge.Kind)
		}
		if !c.Edge.Applicability.Has(kind) {
			if c.Edge.Assert {
				return rejected(f, "type", "value is "+kindName(kind)+
					", schema asserts "+kindSetName(c.Edge.Applicability)), nil
			}
			continue
		}
		out, err := in.predicate(c.Predicate, value, f)
		if err != nil || !out.Accepted {
			return out, err
		}
	}
	return accepted(), nil
}

//nolint:gocyclo // one case per plan.PredicateExpr variant; splitting it would only hide the exhaustiveness.
func (in *interp) predicate(e plan.PredicateExpr, value any, f frame) (Verdict, error) {
	if e == nil {
		return accepted(), nil
	}

	switch e := e.(type) {
	case plan.MinLengthPredicate:
		return lengthBound(f, value, e.Value, true)

	case plan.MaxLengthPredicate:
		return lengthBound(f, value, e.Value, false)

	case plan.PatternPredicate:
		s, ok := value.(string)
		if !ok {
			return accepted(), nil
		}
		if in.matchPattern(e.Regex, s) == patternNoMatch {
			return rejected(f, "pattern", strconv.Quote(e.Regex)+": no match"), nil
		}
		return accepted(), nil

	case plan.FormatPredicate:
		// `format` is an annotation in the 2020-12 standard dialect: assertion requires
		// opting into format-assertion (docs/integration.md §1.1), and the plan carries
		// no such opt-in, so there is nothing here to enforce.
		in.approximate("format " + strconv.Quote(e.Format) + " is not asserted")
		return accepted(), nil

	case plan.MinimumPredicate:
		return numericBound(f, value, e.Value, e.Exclusive, true)

	case plan.MaximumPredicate:
		return numericBound(f, value, e.Value, e.Exclusive, false)

	case plan.MultipleOfPredicate:
		return multipleOf(f, value, e.Value)

	case plan.MinItemsPredicate:
		return itemsBound(f, value, e.Value, true)

	case plan.MaxItemsPredicate:
		return itemsBound(f, value, e.Value, false)

	case plan.UniqueItemsPredicate:
		return uniqueItems(f, value)

	case plan.ContainsCountPredicate:
		return in.containsCount(e.Schema, e.Min, e.Max, value, f)

	case plan.RequiredPredicate:
		obj, ok := value.(map[string]any)
		if !ok {
			return accepted(), nil
		}
		for _, name := range e.Properties {
			if _, present := obj[name]; !present {
				return rejected(f, "required", strconv.Quote(name)+" is absent"), nil
			}
		}
		return accepted(), nil

	case plan.MinPropertiesPredicate:
		return propertiesBound(f, value, e.Value, true)

	case plan.MaxPropertiesPredicate:
		return propertiesBound(f, value, e.Value, false)

	case plan.DependentRequiredPredicate:
		return dependentRequired(f, e.Entries, value)

	case plan.NegationPredicate:
		return in.negation(e.Schema, value, f)

	case plan.PropertyNamesPredicate:
		return in.propertyNames(e.Schema, value, f)

	case plan.ShapePredicate:
		return in.shape(e.Schema, value, f)

	default:
		return Verdict{}, internalf("unhandled plan.PredicateExpr variant %T", e)
	}
}

func lengthBound(f frame, value any, bound uint64, isMin bool) (Verdict, error) {
	s, ok := value.(string)
	if !ok {
		return accepted(), nil
	}
	n := uint64(utf8.RuneCountInString(s)) //nolint:gosec // a rune count is never negative.
	if isMin && n < bound {
		return rejected(f, "minLength", strconv.FormatUint(bound, 10)+": length "+strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected(f, "maxLength", strconv.FormatUint(bound, 10)+": length "+strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func numericBound(f frame, value any, bound float64, exclusive, isMin bool) (Verdict, error) {
	got, err := ratOf(value)
	if err != nil {
		return accepted(), nil //nolint:nilerr // non-numbers are out of this predicate's guard.
	}
	want, err := ratOfFloat(bound)
	if err != nil {
		return Verdict{}, err
	}
	cmp := got.Cmp(want)
	bad := false
	name := "minimum"
	switch {
	case isMin && exclusive:
		bad, name = cmp <= 0, "exclusiveMinimum"
	case isMin:
		bad = cmp < 0
	case exclusive:
		bad, name = cmp >= 0, "exclusiveMaximum"
	default:
		bad, name = cmp > 0, "maximum"
	}
	if bad {
		return rejected(f, name, want.RatString()+": value "+got.RatString()), nil
	}
	return accepted(), nil
}

func multipleOf(f frame, value any, divisor float64) (Verdict, error) {
	got, err := ratOf(value)
	if err != nil {
		return accepted(), nil //nolint:nilerr // non-numbers are out of this predicate's guard.
	}
	want, err := ratOfFloat(divisor)
	if err != nil {
		return Verdict{}, err
	}
	if want.Sign() == 0 {
		return Verdict{}, internalf("multipleOf 0")
	}
	if !new(big.Rat).Quo(got, want).IsInt() {
		return rejected(f, "multipleOf", want.RatString()+": value "+got.RatString()), nil
	}
	return accepted(), nil
}

func itemsBound(f frame, value any, bound uint64, isMin bool) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}
	n := uint64(len(items))
	if isMin && n < bound {
		return rejected(f, "minItems", strconv.FormatUint(bound, 10)+": length "+strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected(f, "maxItems", strconv.FormatUint(bound, 10)+": length "+strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func propertiesBound(f frame, value any, bound uint64, isMin bool) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	n := uint64(len(obj))
	if isMin && n < bound {
		return rejected(f, "minProperties", strconv.FormatUint(bound, 10)+": "+strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected(f, "maxProperties", strconv.FormatUint(bound, 10)+": "+strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func uniqueItems(f frame, value any) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			eq, err := equalValues(items[i], items[j])
			if err != nil {
				return Verdict{}, withPath(f.path, err)
			}
			if eq {
				return rejected(f, "uniqueItems",
					"items "+strconv.Itoa(i)+" and "+strconv.Itoa(j)+" are equal"), nil
			}
		}
	}
	return accepted(), nil
}

func dependentRequired(f frame, entries []plan.DependentRequiredEntry, value any) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	for _, entry := range entries {
		// plan.DependentRequiredEntry is not a node planwalk descends into, so the
		// binding guard on it lives here.
		var t struct {
			Property string
			Requires []string
		} = entry
		if _, present := obj[t.Property]; !present {
			continue
		}
		for _, need := range t.Requires {
			if _, present := obj[need]; !present {
				return rejected(f, "dependentRequired["+strconv.Quote(t.Property)+"]",
					strconv.Quote(need)+" is absent"), nil
			}
		}
	}
	return accepted(), nil
}

// containsCount is the element-wise match-count of docs/integration.md §3.
func (in *interp) containsCount(schema plan.CompilationPlan, minCount uint64, maxCount *uint64, value any, f frame) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}
	var n uint64
	var last *ValidateError
	for i, item := range items {
		v, err := in.plan(schema, item, f.descend(strconv.Itoa(i)))
		if err != nil {
			return Verdict{}, err
		}
		if v.Accepted {
			n++
			continue
		}
		last = v.Reason
	}
	if n < minCount {
		return rejectedBy(f, "contains", strconv.FormatUint(n, 10)+" matches, want at least "+
			strconv.FormatUint(minCount, 10), last), nil
	}
	if maxCount != nil && n > *maxCount {
		return rejected(f, "contains", strconv.FormatUint(n, 10)+" matches, want at most "+
			strconv.FormatUint(*maxCount, 10)), nil
	}
	return accepted(), nil
}

// negation runs the negated sub-plan against the whole instance and inverts the verdict.
// The planner only emits a [plan.NegationPredicate] over a sub-plan it proved exact
// (design §11.8), so the plan itself is exact — but the interpreter has constraints of its
// own it cannot enforce, and inverting a verdict reached over one of them turns an
// over-acceptance into a rejection of a valid instance, which §24 forbids. An acceptance
// the sub-run had to approximate therefore yields an acceptance here too, which only ever
// widens.
func (in *interp) negation(schema plan.CompilationPlan, value any, f frame) (Verdict, error) {
	before := len(in.approx)
	v, err := in.plan(schema, value, f.here())
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted || len(in.approx) > before {
		return accepted(), nil
	}
	return rejected(f, "not", "the negated schema accepts this instance"), nil
}

// shape runs a kind-restricted sub-plan against the whole instance. The enclosing
// [plan.GuardedPredicate] has already decided the instance's kind is in the guard, so the
// sub-plan's own representation is the one a sibling `type` would have produced
// (design §3).
func (in *interp) shape(schema plan.CompilationPlan, value any, f frame) (Verdict, error) {
	v, err := in.plan(schema, value, f.here())
	if err != nil || v.Accepted {
		return v, err
	}
	return rejectedBy(f, "shape", "the instance does not match the kind-guarded shape", v.Reason), nil
}

func (in *interp) propertyNames(schema plan.CompilationPlan, value any, f frame) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	for name := range obj {
		v, err := in.plan(schema, name, f.here())
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, "propertyNames", strconv.Quote(name), v.Reason), nil
		}
	}
	return accepted(), nil
}
