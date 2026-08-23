package nodetree

import (
	"math/big"
	"strconv"
	"unicode/utf8"

	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
	"github.com/ogen-go/schemacompiler/internal/jsonequal"
	"github.com/ogen-go/schemacompiler/plan"
)

type always struct{}

func (always) ok([]byte) bool { return true }

// all is a conjunction, short-circuiting on the first rejection.
type all []node

func (a all) ok(raw []byte) bool {
	for _, n := range a {
		if !n.ok(raw) {
			return false
		}
	}
	return true
}

// guard applies inner only when the instance's kind is in set; a kind outside it
// contributes nothing (design §3.1).
type guard struct {
	set   plan.KindSet
	inner node
}

func (g guard) ok(raw []byte) bool {
	k, valid := kindOf(raw)
	if !valid {
		return false
	}
	if !g.set.Has(k) {
		return true
	}
	return g.inner.ok(raw)
}

// assertKind rejects an instance whose kind is outside set, which is the other reading of
// Applicability (design §3.1). inner may be nil: `type` alone is the whole check.
type assertKind struct {
	set   plan.KindSet
	inner node
}

func (a assertKind) ok(raw []byte) bool {
	k, valid := kindOf(raw)
	if !valid || !a.set.Has(k) {
		return false
	}
	if a.inner == nil {
		return true
	}
	return a.inner.ok(raw)
}

// ---- string ----

type lengthBound struct {
	bound uint64
	isMin bool
}

func (l lengthBound) ok(raw []byte) bool {
	s, ok := decodeString(raw)
	if !ok {
		return true
	}
	n := uint64(utf8.RuneCount(s)) //nolint:gosec // a rune count is never negative.
	if l.isMin {
		return n >= l.bound
	}
	return n <= l.bound
}

type patternNode struct{ regex string }

func (p patternNode) ok(raw []byte) bool {
	s, ok := decodeString(raw)
	if !ok {
		return true
	}
	// An engine that cannot decide the pattern must not be the reason an instance is
	// rejected (design §24), matching planterp.
	res, err := ecmaregex.MatchString(p.regex, string(s))
	return err != nil || res != ecmaregex.NoMatch
}

func decodeString(raw []byte) ([]byte, bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	s, err := d.StrBytes()
	if err != nil {
		return nil, false
	}
	return s, true
}

// ---- number ----

type numericBound struct {
	want      *big.Rat
	exclusive bool
	isMin     bool
	// fast is want as an int64 when it is integral and small, which covers most schemas
	// and avoids a big.Rat allocation per instance.
	fast    int64
	hasFast bool
}

func newNumericBound(value float64, exclusive, isMin bool) (numericBound, bool) {
	want := new(big.Rat)
	if _, ok := want.SetString(strconv.FormatFloat(value, 'g', -1, 64)); !ok {
		return numericBound{}, false
	}
	n := numericBound{want: want, exclusive: exclusive, isMin: isMin}
	if want.IsInt() && want.Num().IsInt64() {
		n.fast, n.hasFast = want.Num().Int64(), true
	}
	return n, true
}

func (n numericBound) ok(raw []byte) bool {
	if n.hasFast {
		if got, ok := fastInt(raw); ok {
			return n.compare(compareInt64(got, n.fast))
		}
	}
	got := new(big.Rat)
	if _, ok := got.SetString(string(trimSpace(raw))); !ok {
		return true
	}
	return n.compare(got.Cmp(n.want))
}

func (n numericBound) compare(cmp int) bool {
	switch {
	case n.isMin && n.exclusive:
		return cmp > 0
	case n.isMin:
		return cmp >= 0
	case n.exclusive:
		return cmp < 0
	default:
		return cmp <= 0
	}
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// fastInt parses raw as an int64 when it is written as a plain integer. Anything with a
// fraction, an exponent, or too many digits falls back to big.Rat.
func fastInt(raw []byte) (int64, bool) {
	b := trimSpace(raw)
	if len(b) == 0 || len(b) > 18 {
		return 0, false
	}
	for _, c := range b {
		if c == '.' || c == 'e' || c == 'E' {
			return 0, false
		}
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func trimSpace(raw []byte) []byte {
	i, j := 0, len(raw)
	for i < j && isSpace(raw[i]) {
		i++
	}
	for j > i && isSpace(raw[j-1]) {
		j--
	}
	return raw[i:j]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

type multipleOf struct{ divisor *big.Rat }

func (m multipleOf) ok(raw []byte) bool {
	got := new(big.Rat)
	if _, ok := got.SetString(string(trimSpace(raw))); !ok {
		return true
	}
	q := new(big.Rat).Quo(got, m.divisor)
	return q.IsInt()
}

// integralFromText decides integrality without parsing when the number is written plainly.
// `1.0` and `1e2` are integers in JSON Schema, so only a decimal point or an exponent
// forces the exact path.
func integralFromText(b []byte) (integral, decided bool) {
	for _, c := range b {
		if c == '.' || c == 'e' || c == 'E' {
			return false, false
		}
	}
	return true, len(b) > 0
}

type numericDomain struct{ domain plan.NumericDomain }

func (n numericDomain) ok(raw []byte) bool {
	if integral, decided := integralFromText(trimSpace(raw)); decided {
		switch n.domain {
		case plan.IntegerOnly:
			return integral
		case plan.NonIntegerOnly:
			return !integral
		default:
			return true
		}
	}
	got := new(big.Rat)
	if _, ok := got.SetString(string(trimSpace(raw))); !ok {
		return true
	}
	switch n.domain {
	case plan.IntegerOnly:
		return got.IsInt()
	case plan.NonIntegerOnly:
		return !got.IsInt()
	default:
		return true
	}
}

// ---- array ----

type itemsBound struct {
	bound uint64
	isMin bool
}

func (i itemsBound) ok(raw []byte) bool {
	n, ok := countElems(raw)
	if !ok {
		return true
	}
	if i.isMin {
		return n >= i.bound
	}
	return n <= i.bound
}

func countElems(raw []byte) (uint64, bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	var n uint64
	if err := d.Arr(func(d *jx.Decoder) error {
		n++
		return d.Skip()
	}); err != nil {
		return 0, false
	}
	return n, true
}

type uniqueItems struct{}

func (uniqueItems) ok(raw []byte) bool {
	items, ok := rawElems(raw)
	if !ok {
		return true
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			eq, err := jsonequal.Equal(items[i], items[j])
			if err != nil {
				return true
			}
			if eq {
				return false
			}
		}
	}
	return true
}

func rawElems(raw []byte) ([][]byte, bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	var out [][]byte
	if err := d.Arr(func(d *jx.Decoder) error {
		r, err := d.Raw()
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	}); err != nil {
		return nil, false
	}
	return out, true
}

// ---- object ----

type requiredNode struct {
	set    nameSet
	wanted presence
}

func (r requiredNode) ok(raw []byte) bool {
	seen, ok := r.set.presenceOf(raw)
	if !ok {
		return true
	}
	return seen.covers(r.wanted)
}

type propertiesBound struct {
	bound uint64
	isMin bool
}

func (p propertiesBound) ok(raw []byte) bool {
	n, ok := countProps(raw)
	if !ok {
		return true
	}
	if p.isMin {
		return n >= p.bound
	}
	return n <= p.bound
}

func countProps(raw []byte) (uint64, bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	var n uint64
	if err := d.ObjBytes(func(d *jx.Decoder, _ []byte) error {
		n++
		return d.Skip()
	}); err != nil {
		return 0, false
	}
	return n, true
}

// dependentEntry is one `dependentRequired` mapping with both sides resolved to
// [nameSet] positions, so validating an instance costs no lookups by name.
type dependentEntry struct {
	trigger  int
	requires []int
}

type dependentRequired struct {
	set     nameSet
	entries []dependentEntry
}

func (dr dependentRequired) ok(raw []byte) bool {
	seen, ok := dr.set.presenceOf(raw)
	if !ok {
		return true
	}
	for _, e := range dr.entries {
		if !seen.has(e.trigger) {
			continue
		}
		for _, i := range e.requires {
			if !seen.has(i) {
				return false
			}
		}
	}
	return true
}

// ---- nested plans ----

type negation struct{ inner node }

func (n negation) ok(raw []byte) bool { return !n.inner.ok(raw) }

type shape struct{ inner node }

func (s shape) ok(raw []byte) bool { return s.inner.ok(raw) }

type propertyNames struct{ inner node }

func (p propertyNames) ok(raw []byte) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	valid := true
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		// The sub-plan sees the name as a JSON string, so it is re-encoded rather than
		// passed as bare bytes.
		if valid && !p.inner.ok(jx.Raw(strconv.Quote(string(key)))) {
			valid = false
		}
		return d.Skip()
	}); err != nil {
		return true
	}
	return valid
}

type containsCount struct {
	inner    node
	min, max uint64
	hasMax   bool
}

func (c containsCount) ok(raw []byte) bool {
	items, ok := rawElems(raw)
	if !ok {
		return true
	}
	var n uint64
	for _, item := range items {
		if c.inner.ok(item) {
			n++
		}
	}
	if n < c.min {
		return false
	}
	return !c.hasMax || n <= c.max
}

// reference reads the definition table at run time, which is what lets a definition refer
// to itself or to one compiled later.
type reference struct {
	name string
	defs map[string]node
}

func (r reference) ok(raw []byte) bool {
	n, ok := r.defs[r.name]
	if !ok || n == nil {
		return true
	}
	return n.ok(raw)
}

func newDependentRequired(entries []plan.DependentRequiredEntry) dependentRequired {
	var names []string
	for _, e := range entries {
		names = append(names, e.Property)
		names = append(names, e.Requires...)
	}
	set := newNameSet(names)

	out := dependentRequired{set: set, entries: make([]dependentEntry, 0, len(entries))}
	for _, e := range entries {
		trigger, ok := set.indexOf(e.Property)
		if !ok {
			continue
		}
		de := dependentEntry{trigger: trigger, requires: make([]int, 0, len(e.Requires))}
		for _, need := range e.Requires {
			if i, ok := set.indexOf(need); ok {
				de.requires = append(de.requires, i)
			}
		}
		out.entries = append(out.entries, de)
	}
	return out
}
