package nodetree

import (
	"math/big"
	"strconv"

	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/internal/jsonequal"
	"github.com/ogen-go/schemacompiler/plan"
)

// hashedCaseThreshold is the number of cases above which a dispatch builds a lookup table
// instead of scanning. The crossover is at two: a scan compares JSON values, which is
// dearer per case than hashing the selector once. See BenchmarkSelectCase.
const hashedCaseThreshold = 1

// scalarKey canonicalizes a non-string scalar so that equal values produce equal keys,
// which is what lets a map stand in for [jsonequal.Equal]. `1`, `1.0` and `1e0` are one
// value and must not become three keys, so a number that is not a plain integer goes
// through big.Rat, whose RatString is canonical and exact.
func scalarKey(raw []byte, kind plan.JSONKind) (string, bool) {
	switch kind {
	case plan.KindNull:
		return "n", true
	case plan.KindBoolean:
		d := decoder(raw)
		defer jx.PutDecoder(d)
		v, err := d.Bool()
		if err != nil {
			return "", false
		}
		return strconv.FormatBool(v), true
	case plan.KindNumber:
		d := decoder(raw)
		defer jx.PutDecoder(d)
		num, err := d.Num()
		if err != nil {
			return "", false
		}
		if v, err := strconv.ParseInt(string(num), 10, 64); err == nil {
			// The integer form agrees with RatString: both render 1, 1.0 and -0 as
			// "1", "1" and "0".
			return strconv.FormatInt(v, 10), true
		}
		r := new(big.Rat)
		if err := r.UnmarshalText(num); err != nil {
			return "", false
		}
		return r.RatString(), true
	default:
		return "", false
	}
}

// caseTable selects the branch whose literal equals a selector. cases is always populated:
// it is the fallback, and the order errors are reported in.
//
// Strings are indexed apart from the other scalars so the lookup can be spelled
// byString[string(bytes)], which the compiler resolves without allocating. Folding them
// into one map behind a discriminating prefix costs an allocation per instance.
type caseTable struct {
	cases    []literalCase
	byString map[string]node
	byScalar map[string]node
	hashed   bool
}

func newCaseTable(cases []literalCase) caseTable {
	t := caseTable{cases: cases}
	if len(cases) <= hashedCaseThreshold {
		return t
	}
	byString, byScalar, ok := indexCases(cases)
	if !ok {
		return t
	}
	t.byString, t.byScalar, t.hashed = byString, byScalar, true
	return t
}

// indexCases builds the lookup tables, reporting false when the cases cannot be keyed and
// the dispatch has to scan.
func indexCases(cases []literalCase) (byString, byScalar map[string]node, ok bool) {
	byString, byScalar = map[string]node{}, map[string]node{}
	for _, c := range cases {
		kind, valid := kindOf(c.raw)
		if !valid {
			return nil, nil, false
		}
		into, key := byScalar, ""
		if kind == plan.KindString {
			s, err := literalString(c.raw)
			if err != nil {
				return nil, nil, false
			}
			into, key = byString, s
		} else {
			k, keyed := scalarKey(c.raw, kind)
			if !keyed {
				// One composite literal and the whole dispatch scans: a map cannot
				// answer for a key it has no way to compute.
				return nil, nil, false
			}
			key = k
		}
		if _, dup := into[key]; dup {
			// Two cases matching the same value is a plan the scan resolves by order.
			return nil, nil, false
		}
		into[key] = c.inner
	}
	return byString, byScalar, true
}

func literalString(raw []byte) (string, error) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	v, err := d.StrBytes()
	if err != nil {
		return "", err
	}
	return string(v), nil
}

// selectCase finds the case whose literal equals selector, comparing as JSON values rather
// than bytes so `1` and `1.0` agree.
func (t caseTable) selectCase(selector []byte) (node, bool) {
	if !t.hashed {
		return t.scanCases(selector)
	}
	kind, valid := kindOf(selector)
	if !valid {
		return nil, false
	}
	if kind == plan.KindString {
		d := decoder(selector)
		defer jx.PutDecoder(d)
		v, err := d.StrBytes()
		if err != nil {
			return nil, false
		}
		n, found := t.byString[string(v)]
		return n, found
	}
	key, ok := scalarKey(selector, kind)
	if !ok {
		// A composite selector cannot equal any of the scalars the table holds.
		return nil, false
	}
	n, found := t.byScalar[key]
	return n, found
}

func (t caseTable) scanCases(selector []byte) (node, bool) {
	for _, c := range t.cases {
		eq, err := jsonequal.Equal(c.raw, selector)
		if err != nil || !eq {
			continue
		}
		return c.inner, true
	}
	return nil, false
}
