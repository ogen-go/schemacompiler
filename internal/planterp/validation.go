package planterp

import (
	"math/big"
	"regexp"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// validation runs the residual, kind-guarded predicates (design §8). A predicate whose
// Applicability does not include the instance's kind passes vacuously.
func (in *interp) validation(v plan.ValidationPlan, value any, f frame) (Verdict, error) {
	var t struct {
		Predicates []plan.GuardedPredicate
	} = v

	kind, err := kindOf(value)
	if err != nil {
		return Verdict{}, err
	}
	for _, gp := range t.Predicates {
		var g struct {
			Applicability plan.KindSet
			Expression    plan.PredicateExpr
		} = gp
		if !g.Applicability.Has(kind) {
			continue
		}
		out, err := in.predicate(g.Expression, value, f)
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
		var t struct{ Value uint64 } = e
		return lengthBound(value, t.Value, true)

	case plan.MaxLengthPredicate:
		var t struct{ Value uint64 } = e
		return lengthBound(value, t.Value, false)

	case plan.PatternPredicate:
		var t struct{ Regex string } = e
		s, ok := value.(string)
		if !ok {
			return accepted(), nil
		}
		if !in.matchPattern(t.Regex, s) {
			return rejected("pattern " + strconv.Quote(t.Regex) + ": no match"), nil
		}
		return accepted(), nil

	case plan.FormatPredicate:
		var t struct{ Format string } = e
		// `format` is an annotation in the 2020-12 standard dialect: assertion requires
		// opting into format-assertion (docs/integration.md §1.1), and the plan carries
		// no such opt-in, so there is nothing here to enforce.
		in.approximate("format " + strconv.Quote(t.Format) + " is not asserted")
		return accepted(), nil

	case plan.MinimumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		return numericBound(value, t.Value, t.Exclusive, true)

	case plan.MaximumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		return numericBound(value, t.Value, t.Exclusive, false)

	case plan.MultipleOfPredicate:
		var t struct{ Value float64 } = e
		return multipleOf(value, t.Value)

	case plan.MinItemsPredicate:
		var t struct{ Value uint64 } = e
		return itemsBound(value, t.Value, true)

	case plan.MaxItemsPredicate:
		var t struct{ Value uint64 } = e
		return itemsBound(value, t.Value, false)

	case plan.UniqueItemsPredicate:
		var t struct{} = e
		_ = t
		return uniqueItems(value)

	case plan.ContainsCountPredicate:
		var t struct {
			Schema plan.CompilationPlan
			Min    uint64
			Max    *uint64
		} = e
		return in.containsCount(t.Schema, t.Min, t.Max, value, f)

	case plan.RequiredPredicate:
		var t struct{ Properties []string } = e
		obj, ok := value.(map[string]any)
		if !ok {
			return accepted(), nil
		}
		for _, name := range t.Properties {
			if _, present := obj[name]; !present {
				return rejected("required: " + strconv.Quote(name) + " is absent"), nil
			}
		}
		return accepted(), nil

	case plan.MinPropertiesPredicate:
		var t struct{ Value uint64 } = e
		return propertiesBound(value, t.Value, true)

	case plan.MaxPropertiesPredicate:
		var t struct{ Value uint64 } = e
		return propertiesBound(value, t.Value, false)

	case plan.DependentRequiredPredicate:
		var t struct{ Entries []plan.DependentRequiredEntry } = e
		return dependentRequired(t.Entries, value)

	case plan.PropertyNamesPredicate:
		var t struct{ Schema plan.CompilationPlan } = e
		return in.propertyNames(t.Schema, value, f)

	default:
		return Verdict{}, errors.Errorf("planterp: unhandled plan.PredicateExpr variant %T", e)
	}
}

func lengthBound(value any, bound uint64, isMin bool) (Verdict, error) {
	s, ok := value.(string)
	if !ok {
		return accepted(), nil
	}
	n := uint64(utf8.RuneCountInString(s)) //nolint:gosec // a rune count is never negative.
	if isMin && n < bound {
		return rejected("minLength " + strconv.FormatUint(bound, 10) + ": length " + strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected("maxLength " + strconv.FormatUint(bound, 10) + ": length " + strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func numericBound(value any, bound float64, exclusive, isMin bool) (Verdict, error) {
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
		return rejected(name + " " + want.RatString() + ": value " + got.RatString()), nil
	}
	return accepted(), nil
}

func multipleOf(value any, divisor float64) (Verdict, error) {
	got, err := ratOf(value)
	if err != nil {
		return accepted(), nil //nolint:nilerr // non-numbers are out of this predicate's guard.
	}
	want, err := ratOfFloat(divisor)
	if err != nil {
		return Verdict{}, err
	}
	if want.Sign() == 0 {
		return Verdict{}, errors.New("planterp: multipleOf 0")
	}
	if !new(big.Rat).Quo(got, want).IsInt() {
		return rejected("multipleOf " + want.RatString() + ": value " + got.RatString()), nil
	}
	return accepted(), nil
}

func itemsBound(value any, bound uint64, isMin bool) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}
	n := uint64(len(items))
	if isMin && n < bound {
		return rejected("minItems " + strconv.FormatUint(bound, 10) + ": length " + strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected("maxItems " + strconv.FormatUint(bound, 10) + ": length " + strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func propertiesBound(value any, bound uint64, isMin bool) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	n := uint64(len(obj))
	if isMin && n < bound {
		return rejected("minProperties " + strconv.FormatUint(bound, 10) + ": " + strconv.FormatUint(n, 10)), nil
	}
	if !isMin && n > bound {
		return rejected("maxProperties " + strconv.FormatUint(bound, 10) + ": " + strconv.FormatUint(n, 10)), nil
	}
	return accepted(), nil
}

func uniqueItems(value any) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			eq, err := equalValues(items[i], items[j])
			if err != nil {
				return Verdict{}, err
			}
			if eq {
				return rejected("uniqueItems: items " + strconv.Itoa(i) + " and " + strconv.Itoa(j) + " are equal"), nil
			}
		}
	}
	return accepted(), nil
}

func dependentRequired(entries []plan.DependentRequiredEntry, value any) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	for _, entry := range entries {
		var t struct {
			Property string
			Requires []string
		} = entry
		if _, present := obj[t.Property]; !present {
			continue
		}
		for _, need := range t.Requires {
			if _, present := obj[need]; !present {
				return rejected("dependentRequired[" + strconv.Quote(t.Property) + "]: " +
					strconv.Quote(need) + " is absent"), nil
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
	for _, item := range items {
		v, err := in.sub(schema, item, f)
		if err != nil {
			return Verdict{}, err
		}
		if v.Accepted {
			n++
		}
	}
	if n < minCount {
		return rejected("contains: " + strconv.FormatUint(n, 10) + " matches, want at least " +
			strconv.FormatUint(minCount, 10)), nil
	}
	if maxCount != nil && n > *maxCount {
		return rejected("contains: " + strconv.FormatUint(n, 10) + " matches, want at most " +
			strconv.FormatUint(*maxCount, 10)), nil
	}
	return accepted(), nil
}

func (in *interp) propertyNames(schema plan.CompilationPlan, value any, f frame) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return accepted(), nil
	}
	for name := range obj {
		v, err := in.sub(schema, name, f)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejected("propertyNames[" + strconv.Quote(name) + "]: " + v.Reason), nil
		}
	}
	return accepted(), nil
}

// matchPattern applies an ECMA-262 pattern as an unanchored search, the way JSON Schema
// defines `pattern` and `patternProperties`. Go's RE2 rejects the constructs ECMA-262
// has and RE2 does not (lookaround, backreferences); such a pattern is recorded as
// unenforceable and matches everything, since refusing to match would under-approximate.
func (in *interp) matchPattern(pattern, s string) bool {
	re, err := compilePattern(pattern)
	if err != nil {
		in.approximate("pattern " + strconv.Quote(pattern) + " does not compile as RE2")
		return true
	}
	return re.MatchString(s)
}

// patternCache memoizes compilation across instances: the suite runs thousands of them
// against the same handful of patterns.
var patternCache sync.Map // string -> *regexp.Regexp or error

func compilePattern(pattern string) (*regexp.Regexp, error) {
	if got, ok := patternCache.Load(pattern); ok {
		if re, ok := got.(*regexp.Regexp); ok {
			return re, nil
		}
		return nil, got.(error)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		patternCache.Store(pattern, err)
		return nil, err
	}
	patternCache.Store(pattern, re)
	return re, nil
}
