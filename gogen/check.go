package gogen

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// A slot that cannot be absent needs no presence test; [presentTest] says so with the
// expression that is always true.
const alwaysPresent = "true"

// rejectBound is the comparison that rejects a value outside a bound, and the words a
// message states the bound with. What is written is the complement of what is admitted.
func rejectBound(lower bool) (op, word string) {
	if lower {
		return "<", "at least"
	}
	return ">", "at most"
}

// vpath is where a checked value sits inside the value being validated, as a format string
// and the arguments a generated error passes with it. A map key or a slice index is only
// known at run time, so the path cannot be a constant.
type vpath struct {
	format string
	args   []string
}

func (p vpath) with(segment string, args ...string) vpath {
	return vpath{format: p.format + segment, args: append(append([]string{}, p.args...), args...)}
}

// static appends a segment that is known at generation time, so no argument goes with it.
func (p vpath) static(segment string) vpath {
	return vpath{format: p.format + strings.ReplaceAll(segment, "%", "%%"), args: p.args}
}

func (p vpath) errorf(msg string) string {
	msg = strings.ReplaceAll(msg, "%", "%%")
	if p.format != "" {
		msg = p.format + ": " + msg
	}
	if len(p.args) == 0 {
		return "errors.New(" + strconv.Quote(strings.ReplaceAll(msg, "%%", "%")) + ")"
	}
	return fmt.Sprintf("fmt.Errorf(%s, %s)", strconv.Quote(msg), strings.Join(p.args, ", "))
}

func (p vpath) wrap(err string) string {
	if p.format == "" {
		return "return " + err
	}
	args := append(append([]string{}, p.args...), err)
	return fmt.Sprintf("return fmt.Errorf(%s, %s)", strconv.Quote(p.format+": %w"), strings.Join(args, ", "))
}

// emittable reports whether a check can be written for gp over t, by writing one and
// seeing whether anything came out. One implementation rather than two, so the pass that
// decides what is enforced and the pass that enforces it cannot disagree.
func emittable(t GoType, gp plan.GuardedPredicate) bool {
	var probe renderer
	return probe.predCode(t, gp, "x", vpath{}) != ""
}

// validator writes the checks the Go type does not make itself (design §20.2).
func (r *renderer) validator(n *Named, v *vnode) {
	fmt.Fprintf(&r.b, "// Validate reports the first constraint s does not satisfy.\nfunc (s %s) Validate() error {\n", n.Name)
	r.checkNode(v, "s", vpath{}, 0)
	r.b.WriteString("return nil\n}\n\n")
}

func (r *renderer) checkNode(v *vnode, expr string, p vpath, depth int) {
	for _, gp := range v.preds {
		r.b.WriteString(r.predCode(v.t, gp, expr, p))
	}
	if v.call != nil {
		if p.format != "" {
			r.need("fmt")
		}
		fmt.Fprintf(&r.b, "if err := %s.Validate(); err != nil {\n%s\n}\n", expr, p.wrap("err"))
	}
	for _, k := range v.kids {
		r.checkChild(v.t, k, expr, p, depth)
	}
}

func (r *renderer) checkChild(parent GoType, k vchild, expr string, p vpath, depth int) {
	b := &r.b
	val, key, idx := "v"+strconv.Itoa(depth), "k"+strconv.Itoa(depth), "i"+strconv.Itoa(depth)
	sel := expr
	if name := selector(parent, k.edge); name != "" {
		sel = expr + "." + name
	}
	rangeOver := func(over, loop string, seg vpath) {
		r.need("fmt")
		fmt.Fprintf(b, "for %s, %s := range %s {\n", loop, val, over)
		r.checkNode(k.node, val, seg, depth+1)
		b.WriteString("}\n")
	}
	switch k.edge.Kind {
	case EdgeStored:
		fmt.Fprintf(b, "if %s, ok := %s.Get(); ok {\n", val, expr)
		r.checkNode(k.node, val, p, depth+1)
		b.WriteString("}\n")
	case EdgePointee:
		fmt.Fprintf(b, "if %s != nil {\n", expr)
		r.checkNode(k.node, "(*"+expr+")", p, depth+1)
		b.WriteString("}\n")
	case EdgeEnumElem:
		// An enum is stored as its element, so there is nothing to reach through.
		r.checkNode(k.node, expr, p, depth)
	case EdgeField:
		r.checkNode(k.node, sel, p.static("."+k.edge.JSON), depth)
	case EdgePattern, EdgeAdditional:
		rangeOver(sel, key, p.with("[%q]", key))
	case EdgeElem:
		if _, ok := deref(parent).(*Map); ok {
			rangeOver(sel, key, p.with("[%q]", key))
			return
		}
		rangeOver(sel, idx, p.with("[%d]", idx))
	case EdgeTupleElem:
		r.checkNode(k.node, sel, p.static("["+strconv.Itoa(k.edge.Index)+"]"), depth)
	case EdgeTupleRest:
		prefix := len(deref(parent).(*Tuple).Elems)
		rangeOver(sel, idx, p.with("[%d]", idx+"+"+strconv.Itoa(prefix)))
	default:
		panic(fmt.Sprintf("gogen: unhandled EdgeKind %v under a validator", k.edge.Kind))
	}
}

// predCode writes gp as a check over a value of type t held in expr, and writes nothing
// when it cannot. Writing nothing is not a silent drop: [vbuilder] admits what it does not
// write, and [Render] says so on the declaration.
func (r *renderer) predCode(t GoType, gp plan.GuardedPredicate, expr string, p vpath) string {
	switch e := gp.Expression.(type) {
	case *plan.MinLengthPredicate:
		return r.length(t, expr, p, e.Value, true)
	case *plan.MaxLengthPredicate:
		return r.length(t, expr, p, e.Value, false)
	case *plan.MinimumPredicate:
		return r.bound(t, expr, p, e.Value, e.Exclusive, true)
	case *plan.MaximumPredicate:
		return r.bound(t, expr, p, e.Value, e.Exclusive, false)
	case *plan.MultipleOfPredicate:
		return r.multipleOf(t, expr, p, e.Value)
	case *plan.NumericDomainPredicate:
		return r.integral(t, expr, p, e.Domain)
	case *plan.MinItemsPredicate:
		return r.itemBound(t, expr, p, e.Value, true)
	case *plan.MaxItemsPredicate:
		return r.itemBound(t, expr, p, e.Value, false)
	case *plan.MinPropertiesPredicate:
		return r.propertyBound(t, expr, p, e.Value, true)
	case *plan.MaxPropertiesPredicate:
		return r.propertyBound(t, expr, p, e.Value, false)
	case *plan.UniqueItemsPredicate:
		return r.unique(t, expr, p)
	case *plan.RequiredPredicate:
		return r.required(t, expr, p, e.Properties)
	case *plan.DependentRequiredPredicate:
		return r.dependentRequired(t, expr, p, e.Entries)
	default:
		return ""
	}
}

// markError records the import the generated error needs, which depends on whether the
// path has arguments to format.
func (r *renderer) markError(p vpath) {
	if len(p.args) == 0 {
		r.need("errors")
		return
	}
	r.need("fmt")
}

func (r *renderer) reject(p vpath, cond, msg string) string {
	r.markError(p)
	return fmt.Sprintf("if %s {\nreturn %s\n}\n", cond, p.errorf(msg))
}

func (r *renderer) length(t GoType, expr string, p vpath, v uint64, lower bool) string {
	if !isStringCore(t) {
		return ""
	}
	r.need("unicode/utf8")
	op, word := rejectBound(lower)
	return r.reject(p, fmt.Sprintf("utf8.RuneCountInString(string(%s)) %s %d", expr, op, v),
		"must be "+word+" "+plural(v, "character", "characters"))
}

func (r *renderer) bound(t GoType, expr string, p vpath, v float64, exclusive, lower bool) string {
	lit, ok := numericLiteral(t, v)
	if !ok {
		return ""
	}
	op, word := rejectBound(lower)
	switch {
	case lower && exclusive:
		op, word = "<=", "greater than"
	case exclusive:
		op, word = ">=", "less than"
	}
	return r.reject(p, fmt.Sprintf("%s %s %s", expr, op, lit), fmt.Sprintf("must be %s %s", word, numberText(v)))
}

// multipleOf is emitted only for a divisor Go can divide by exactly.
//
// `planterp` answers this in [math/big.Rat] because binary floating point cannot: 1e-08 is
// not the number the document wrote, so `math.Mod(12391239123, 1e-08)` is not zero and the
// check would reject a value the schema admits — the one direction design §24 forbids. An
// integral divisor has no such gap, and everything else is declared rather than guessed at.
func (r *renderer) multipleOf(t GoType, expr string, p vpath, v float64) string {
	if !isNumberCore(t) || v == 0 || v != math.Trunc(v) {
		return ""
	}
	msg := "must be a multiple of " + numberText(v)
	if isIntCore(t) {
		return r.reject(p, fmt.Sprintf("%s%%%d != 0", expr, int64(v)), msg)
	}
	r.need("math")
	return r.reject(p, fmt.Sprintf("math.Mod(float64(%s), %s) != 0", expr, trimFloat(v)), msg)
}

func (r *renderer) integral(t GoType, expr string, p vpath, d plan.NumericDomain) string {
	if d != plan.IntegerOnly || !isNumberCore(t) || isIntCore(t) {
		return ""
	}
	r.need("math")
	return r.reject(p, fmt.Sprintf("%s != math.Trunc(%s)", expr, expr), "must be an integer")
}

func (r *renderer) itemBound(t GoType, expr string, p vpath, v uint64, lower bool) string {
	op, word := rejectBound(lower)
	msg := "must have " + word + " " + plural(v, "item", "items")
	switch a := deref(t).(type) {
	case *Slice:
		return r.reject(p, fmt.Sprintf("len(%s) %s %d", expr, op, v), msg)
	case *Tuple:
		r.markError(p)
		var b strings.Builder
		b.WriteString("{\n")
		b.WriteString(tupleCount(a, expr))
		fmt.Fprintf(&b, "if count %s %d {\nreturn %s\n}\n}\n", op, v, p.errorf(msg))
		return b.String()
	default:
		return ""
	}
}

// tupleCount writes the number of items a tuple encodes as. Encoding stops at the first
// absent slot (§13), so the count is the leading run of present slots, and the rest is
// reached only when every slot before it is present.
func tupleCount(t *Tuple, expr string) string {
	var b strings.Builder
	b.WriteString("count := 0\n")
	var open int
	for i := range t.Elems {
		if pres, ok := t.Elems[i].(*Presence); ok && pres.Optional {
			fmt.Fprintf(&b, "if %s {\n", presentExpr(expr+"."+slotsOfTuple(t).Elems[i], pres))
			open++
		}
		b.WriteString("count++\n")
	}
	if t.Rest != nil {
		fmt.Fprintf(&b, "count += len(%s.%s)\n", expr, slotsOfTuple(t).Rest)
	}
	b.WriteString(strings.Repeat("}\n", open))
	return b.String()
}

func (r *renderer) propertyBound(t GoType, expr string, p vpath, v uint64, lower bool) string {
	op, word := rejectBound(lower)
	msg := "must have " + word + " " + plural(v, "property", "properties")
	switch o := deref(t).(type) {
	case *Map:
		return r.reject(p, fmt.Sprintf("len(%s) %s %d", expr, op, v), msg)
	case *Struct:
		r.markError(p)
		var b strings.Builder
		b.WriteString("{\ncount := 0\n")
		for _, f := range o.Fields {
			if pres, ok := f.Type.(*Presence); ok && pres.Optional {
				fmt.Fprintf(&b, "if %s {\ncount++\n}\n", presentExpr(expr+"."+f.Name, pres))
				continue
			}
			b.WriteString("count++\n")
		}
		for i := range o.Patterns {
			fmt.Fprintf(&b, "count += len(%s.%s)\n", expr, slotsOf(o).Patterns[i])
		}
		if o.Additional != nil {
			fmt.Fprintf(&b, "count += len(%s.%s)\n", expr, slotsOf(o).Additional)
		}
		fmt.Fprintf(&b, "if count %s %d {\nreturn %s\n}\n}\n", op, v, p.errorf(msg))
		return b.String()
	default:
		return ""
	}
}

func (r *renderer) unique(t GoType, expr string, p vpath) string {
	s, ok := deref(t).(*Slice)
	if !ok {
		return ""
	}
	if _, ok := deref(s.Elem).(*Primitive); !ok {
		return ""
	}
	r.markError(p)
	var b strings.Builder
	fmt.Fprintf(&b, "{\nseen := make(map[%s]struct{}, len(%s))\n", TypeExpr(s.Elem), expr)
	fmt.Fprintf(&b, "for _, v := range %s {\nif _, dup := seen[v]; dup {\nreturn %s\n}\nseen[v] = struct{}{}\n}\n}\n",
		expr, p.errorf("must not contain duplicate items"))
	return b.String()
}

func (r *renderer) required(t GoType, expr string, p vpath, names []string) string {
	s, ok := deref(t).(*Struct)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, name := range names {
		test, ok := presentTest(s, expr, name)
		if !ok {
			// Presence lives in the overflow map, which only a decoder can have filled.
			return ""
		}
		if test == alwaysPresent {
			continue
		}
		r.markError(p)
		fmt.Fprintf(&b, "if %s {\nreturn %s\n}\n", negate(test),
			p.errorf("property "+strconv.Quote(name)+" is required"))
	}
	return b.String()
}

func (r *renderer) dependentRequired(t GoType, expr string, p vpath, entries []plan.DependentRequiredEntry) string {
	s, ok := deref(t).(*Struct)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		trigger, ok := presentTest(s, expr, entry.Property)
		if !ok {
			return ""
		}
		var inner strings.Builder
		for _, name := range entry.Requires {
			test, ok := presentTest(s, expr, name)
			if !ok {
				return ""
			}
			if test == alwaysPresent {
				continue
			}
			r.markError(p)
			fmt.Fprintf(&inner, "if %s {\nreturn %s\n}\n", negate(test),
				p.errorf("property "+strconv.Quote(name)+" is required when "+strconv.Quote(entry.Property)+" is present"))
		}
		switch {
		case inner.Len() == 0:
		case trigger == alwaysPresent:
			b.WriteString(inner.String())
		default:
			fmt.Fprintf(&b, "if %s {\n%s}\n", trigger, inner.String())
		}
	}
	return b.String()
}

func presentTest(s *Struct, expr, name string) (string, bool) {
	i := fieldIndex(s, name)
	if i < 0 {
		return "", false
	}
	pres, ok := s.Fields[i].Type.(*Presence)
	if !ok || !pres.Optional {
		return alwaysPresent, true
	}
	return presentExpr(expr+"."+s.Fields[i].Name, pres), true
}

func negate(test string) string {
	if after, ok := strings.CutPrefix(test, "!"); ok {
		return after
	}
	return "!" + test
}

// presentExpr is whether the property was there, which is not whether a value was: a null
// admitted beside a value is still a present property (design §7.1).
func presentExpr(sel string, pres *Presence) string {
	if pres.Nullable {
		return "!" + sel + ".IsAbsent()"
	}
	return sel + ".IsSet()"
}

func numericLiteral(t GoType, v float64) (string, bool) {
	if !isNumberCore(t) {
		return "", false
	}
	if isIntCore(t) {
		if v != math.Trunc(v) || math.Abs(v) > math.MaxInt64 {
			return "", false
		}
		return strconv.FormatInt(int64(v), 10), true
	}
	return trimFloat(v), true
}

// numberText is the value as a reader would write it; trimFloat is the same value as Go
// source, which needs a decimal point a message does not.
func plural(n uint64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.FormatUint(n, 10) + " " + many
}

func numberText(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func isStringCore(t GoType) bool {
	p, ok := deref(t).(*Primitive)
	return ok && p.Kind == PrimitiveString
}

func isNumberCore(t GoType) bool {
	p, ok := deref(t).(*Primitive)
	return ok && (p.Kind == PrimitiveInt || p.Kind == PrimitiveFloat)
}

func isIntCore(t GoType) bool {
	p, ok := deref(t).(*Primitive)
	return ok && p.Kind == PrimitiveInt
}
