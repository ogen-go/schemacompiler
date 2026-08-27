package gogen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// accessKind is how a checked value is reached from the value holding it.
type accessKind uint8

const (
	accessPresence accessKind = iota
	accessPointer
	accessField
	accessMapValue
	accessSliceElem
	accessTupleElem
	accessTupleRest
)

// vnode is one value's checks: what to test on it, whether it is a declared type that
// tests itself, and the values reachable from it that carry checks of their own.
//
// It is data rather than source text, for docs/backend.md §4's reason: a renderer reading
// it can be syntactically wrong, which a compiler catches, but not semantically wrong.
type vnode struct {
	t     GoType
	preds []plan.GuardedPredicate
	call  *Named
	kids  []vchild
}

type vchild struct {
	kind  accessKind
	sel   string
	json  string
	index int
	node  *vnode
}

// vbuilder pairs the Go type tree with the plan tree. The plans are reachable from the
// predicates themselves — [plan.ObjectStructurePredicate] carries its properties' plans —
// so no map of definitions is needed, and a `$ref` stays a name rather than an inlined
// tree.
type vbuilder struct {
	unenforced map[string]int
}

func (vb *vbuilder) admit(reason string) {
	if vb.unenforced == nil {
		vb.unenforced = map[string]int{}
	}
	vb.unenforced[reason]++
}

func (vb *vbuilder) node(t GoType, c Checks) *vnode {
	switch x := t.(type) {
	case *Named:
		return &vnode{call: x}
	case *Pointer:
		return vb.wrap(accessPointer, x.Elem, c)
	case *Presence:
		return vb.wrap(accessPresence, x.Elem, c)
	case *Enum:
		// The admitted literals are enforced by the decoder, not here (§13); what is
		// left is whatever the element itself carries.
		return vb.node(x.Elem, c)
	}

	n := &vnode{t: t}
	if c.Dispatch.Disposition != Discharged {
		vb.admit(c.Dispatch.Kind.String() + " selects a branch nothing generated evaluates")
	}
	for _, gp := range c.Delegate {
		vb.admit(predicateName(gp.Expression) + " needs the raw document")
	}
	for _, gp := range c.Inline {
		switch e := gp.Expression.(type) {
		case *plan.ObjectStructurePredicate:
			n.kids = append(n.kids, vb.object(t, e)...)
		case *plan.ArrayStructurePredicate:
			n.kids = append(n.kids, vb.array(t, e)...)
		case *plan.ReferencePredicate:
			vb.admit("a reference the Go type does not name")
		default:
			if emittable(t, gp) {
				n.preds = append(n.preds, gp)
			} else {
				vb.admit(predicateName(gp.Expression) + " has no generated check yet")
			}
		}
	}
	return n
}

func (vb *vbuilder) wrap(kind accessKind, elem GoType, c Checks) *vnode {
	inner := vb.node(elem, c)
	if inner.empty() {
		return &vnode{}
	}
	return &vnode{kids: []vchild{{kind: kind, node: inner}}}
}

// sub builds the checks of a sub-plan against the Go type that stores it. Splitting again
// is the whole point: the disposition is a property of the pair (docs/backend.md §2), and
// the pair at this depth is not the one the declaration was split against.
//
// The presence wrappers come off first. Whether a slot may be absent or null is the
// enclosing shape's statement about the slot (design §7.1) — [plan.PropertyCheck] carries
// it beside the plan, not inside it — so splitting the sub-plan against a wrapped type
// would pair a value that admits null with a plan that asserts it is a string.
func (vb *vbuilder) sub(t GoType, p plan.CompilationPlan) *vnode {
	switch x := t.(type) {
	case *Named:
		return &vnode{call: x}
	case *Pointer:
		return vb.wrapSub(accessPointer, x.Elem, p)
	case *Presence:
		return vb.wrapSub(accessPresence, x.Elem, p)
	}
	return vb.node(t, Split(t, p))
}

func (vb *vbuilder) wrapSub(kind accessKind, elem GoType, p plan.CompilationPlan) *vnode {
	inner := vb.sub(elem, p)
	if inner.empty() {
		return &vnode{}
	}
	return &vnode{kids: []vchild{{kind: kind, node: inner}}}
}

func (vb *vbuilder) object(t GoType, e *plan.ObjectStructurePredicate) []vchild {
	switch s := deref(t).(type) {
	case *Struct:
		return vb.structObject(s, e)
	case *Map:
		return vb.mapObject(s, e)
	default:
		vb.admit("an object structure over a type that stores no object")
		return nil
	}
}

func (vb *vbuilder) structObject(s *Struct, e *plan.ObjectStructurePredicate) []vchild {
	var kids []vchild
	for _, pc := range e.Properties {
		i := fieldIndex(s, pc.Name)
		if i < 0 {
			vb.admit("a declared property no field stores")
			continue
		}
		if pres, ok := s.Fields[i].Type.(*Presence); ok && pres.Nullable && !pc.Nullable {
			vb.admit("a null the Go type stores beside a property that does not admit one")
		}
		if n := vb.sub(s.Fields[i].Type, pc.Plan); !n.empty() {
			kids = append(kids, vchild{kind: accessField, sel: s.Fields[i].Name, json: pc.Name, node: n})
		}
	}
	for _, pc := range e.Patterns {
		i := patternIndex(s, pc.Pattern)
		if i < 0 {
			vb.admit("a pattern rule no map stores")
			continue
		}
		if n := vb.sub(s.Patterns[i].Elem, pc.Plan); !n.empty() {
			kids = append(kids, vchild{kind: accessMapValue, sel: patternName(s, i), node: n})
		}
	}
	if e.Additional != nil && s.Additional != nil {
		if n := vb.sub(s.Additional, *e.Additional); !n.empty() {
			kids = append(kids, vchild{kind: accessMapValue, sel: overflowName(s), node: n})
		}
	}
	return kids
}

func (vb *vbuilder) mapObject(m *Map, e *plan.ObjectStructurePredicate) []vchild {
	if len(e.Properties) > 0 {
		vb.admit("a declared property inside a map, which holds one element type for every key")
		return nil
	}
	var p *plan.CompilationPlan
	switch {
	case m.Pattern != "":
		for _, pc := range e.Patterns {
			if pc.Pattern == m.Pattern {
				p = &pc.Plan
			}
		}
	case len(e.Patterns) == 0:
		p = e.Additional
	}
	if p == nil {
		vb.admit("an object structure a map cannot be paired with")
		return nil
	}
	n := vb.sub(m.Elem, *p)
	if n.empty() {
		return nil
	}
	return []vchild{{kind: accessMapValue, node: n}}
}

func (vb *vbuilder) array(t GoType, e *plan.ArrayStructurePredicate) []vchild {
	switch a := deref(t).(type) {
	case *Slice:
		if len(e.Prefix) > 0 {
			vb.admit("a prefix item inside a slice, which holds one element type for every index")
			return nil
		}
		if e.Rest == nil {
			return nil
		}
		n := vb.sub(a.Elem, *e.Rest)
		if n.empty() {
			return nil
		}
		return []vchild{{kind: accessSliceElem, node: n}}
	case *Tuple:
		var kids []vchild
		for i, p := range e.Prefix {
			if i >= len(a.Elems) {
				vb.admit("a prefix item no tuple slot stores")
				continue
			}
			if n := vb.sub(a.Elems[i], p); !n.empty() {
				kids = append(kids, vchild{kind: accessTupleElem, sel: tupleField(a, i), index: i, node: n})
			}
		}
		if e.Rest != nil && a.Rest != nil {
			if n := vb.sub(a.Rest, *e.Rest); !n.empty() {
				kids = append(kids, vchild{kind: accessTupleRest, sel: tupleRestName(a), index: len(a.Elems), node: n})
			}
		}
		return kids
	default:
		vb.admit("an array structure over a type that stores no array")
		return nil
	}
}

func (n *vnode) empty() bool {
	if n == nil {
		return true
	}
	if len(n.preds) > 0 || n.call != nil {
		return false
	}
	for _, k := range n.kids {
		if !k.node.empty() {
			return false
		}
	}
	return true
}

// prune drops the calls to declared types that need no validator, and reports whether
// anything is left. Which calls are worth making is what the fixpoint in [validators]
// decides, and a call to a method that was not generated does not compile.
func (n *vnode) prune(needs map[*Named]bool) bool {
	if n == nil {
		return false
	}
	if n.call != nil && !needs[n.call] {
		n.call = nil
	}
	kids := n.kids[:0]
	for _, k := range n.kids {
		if k.node.prune(needs) {
			kids = append(kids, k)
		}
	}
	n.kids = kids
	return len(n.preds) > 0 || n.call != nil || len(n.kids) > 0
}

// calls collects every declared type this node would call, so the fixpoint can ask what a
// node depends on without rendering it.
func (n *vnode) calls(into map[*Named]bool) {
	if n == nil {
		return
	}
	if n.call != nil {
		into[n.call] = true
	}
	for _, k := range n.kids {
		k.node.calls(into)
	}
}

// validators builds one check tree per declared type and decides which of them need a
// method.
//
// A type needs one when it has a check of its own, or when it reaches one through a type
// that does — which is a least fixpoint, and must be: starting from "everything needs one"
// would keep a whole recursive cycle whose members check nothing.
func validators(types []*Named) (nodes map[*Named]*vnode, needs map[*Named]bool, unenforced map[string]int) {
	var vb vbuilder
	nodes = make(map[*Named]*vnode, len(types))
	deps := make(map[*Named]map[*Named]bool, len(types))
	needs = make(map[*Named]bool, len(types))
	for _, n := range types {
		v := vb.node(n.Underlying, n.Checks)
		nodes[n] = v
		d := map[*Named]bool{}
		v.calls(d)
		deps[n] = d
		needs[n] = hasOwnCheck(v)
	}
	for changed := true; changed; {
		changed = false
		for _, n := range types {
			if needs[n] {
				continue
			}
			for m := range deps[n] {
				if needs[m] {
					needs[n] = true
					changed = true
					break
				}
			}
		}
	}
	for _, n := range types {
		// A node kept by the fixpoint always survives pruning: it has a check of its own,
		// or it reaches a type that does and the call to it stays.
		nodes[n].prune(needs)
	}
	return nodes, needs, vb.unenforced
}

// hasOwnCheck reports whether the tree tests anything without help from another declared
// type's method.
func hasOwnCheck(n *vnode) bool {
	if len(n.preds) > 0 {
		return true
	}
	for _, k := range n.kids {
		if hasOwnCheck(k.node) {
			return true
		}
	}
	return false
}

func patternIndex(s *Struct, pattern string) int {
	for i, p := range s.Patterns {
		if p.Pattern == pattern {
			return i
		}
	}
	return -1
}

func patternName(s *Struct, want int) string {
	taken := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		taken[f.Name] = true
	}
	var name string
	for i := range s.Patterns {
		name = freeName("Pattern"+strconv.Itoa(i)+"Props", taken)
		taken[name] = true
		if i == want {
			return name
		}
	}
	return name
}

func tupleRestName(t *Tuple) string {
	return freeName("Rest", tupleTaken(t))
}

func predicateName(e plan.PredicateExpr) string {
	if e == nil {
		return "a kind guard"
	}
	name := fmt.Sprintf("%T", e)
	name = strings.TrimPrefix(name, "*plan.")
	return "`" + strings.TrimSuffix(name, "Predicate") + "`"
}

// Unenforced is what generated validation leaves undone, by reason and count. Reading it
// is how a caller learns that generated code over-accepts, which design §24 permits only
// while it is declared.
func Unenforced(types []*Named) map[string]int {
	_, _, u := validators(types)
	return u
}

// unenforcedNote writes what no check was generated for. Silence here would be the failure
// #161 is about: a caller reading no admission concludes nothing was left undone.
func (r *renderer) unenforcedNote(unenforced map[string]int) {
	if len(unenforced) == 0 {
		return
	}
	reasons := make([]string, 0, len(unenforced))
	for reason := range unenforced {
		reasons = append(reasons, reason)
	}
	slices.SortFunc(reasons, func(a, b string) int {
		if d := unenforced[b] - unenforced[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	r.b.WriteString("// Not enforced by the generated validators:\n")
	for _, reason := range reasons {
		fmt.Fprintf(&r.b, "//   %5d %s\n", unenforced[reason], reason)
	}
	r.b.WriteString("\n")
}
