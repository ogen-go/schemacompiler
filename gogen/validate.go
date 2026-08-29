package gogen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
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

// vchild is a checked value and the [Edge] it hangs off its holder by. The edge is the
// walk's own vocabulary rather than a second one: how a value is reached is a property of
// the Go type, and [Children] already answers it.
type vchild struct {
	edge Edge
	node *vnode
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
	if named, ok := t.(*Named); ok {
		return &vnode{t: t, call: named}
	}
	if e, elem, ok := boxed(t); ok {
		return vb.wrap(t, e, vb.node(elem, c))
	}
	if enum, ok := t.(*Enum); ok {
		// The literals are the decoder's (§13). What matters here is that c was split
		// against the enum, which is what discharges the dispatch selecting them, so the
		// element is walked with the same checks rather than re-split against itself.
		for n := range Children(enum) {
			return vb.wrap(t, n.Edge, vb.node(n.Type, c))
		}
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

// boxed reports that t holds its value behind a slot the enclosing shape decided on rather
// than the value's own plan — presence and nullability (design §7.1), and the indirection
// the recursion pass introduced. The checks are about the value, not the box.
//
// An enum is not one of these: the literals are part of what the value is, and the split
// that discharges them is taken against the enum itself.
func boxed(t GoType) (Edge, GoType, bool) {
	switch t.(type) {
	case *Presence, *Pointer:
		for n := range Children(t) {
			return n.Edge, n.Type, true
		}
	}
	return Edge{}, nil, false
}

func (vb *vbuilder) wrap(t GoType, e Edge, inner *vnode) *vnode {
	if inner.empty() {
		return &vnode{t: t}
	}
	return &vnode{t: t, kids: []vchild{{edge: e, node: inner}}}
}

// sub builds the checks of a sub-plan against the Go type that stores it. Splitting again
// is the whole point: the disposition is a property of the pair (docs/backend.md §2), and
// the pair at this depth is not the one the declaration was split against.
//
// The boxes come off first. Whether a slot may be absent or null is the enclosing shape's
// statement about the slot (design §7.1) — [plan.PropertyCheck] carries it beside the plan,
// not inside it — so splitting the sub-plan against a wrapped type would pair a value that
// admits null with a plan that asserts it is a string.
func (vb *vbuilder) sub(t GoType, p plan.CompilationPlan) *vnode {
	if named, ok := t.(*Named); ok {
		return &vnode{t: t, call: named}
	}
	if e, elem, ok := boxed(t); ok {
		return vb.wrap(t, e, vb.sub(elem, p))
	}
	return vb.node(t, Split(t, p))
}

// slots indexes a type's children by the edge that reaches them, so a plan's properties
// and items can be looked up by the name or position the plan states them with.
type slots struct {
	fields     map[string]Node
	patterns   map[string]Node
	tuple      []Node
	additional *Node
	elem       *Node
	rest       *Node
}

func childSlots(t GoType) slots {
	s := slots{fields: map[string]Node{}, patterns: map[string]Node{}}
	for n := range Children(t) {
		switch n.Edge.Kind {
		case EdgeField:
			s.fields[n.Edge.JSON] = n
		case EdgePattern:
			s.patterns[n.Edge.Name] = n
		case EdgeAdditional:
			s.additional = &n
		case EdgeElem:
			s.elem = &n
		case EdgeTupleElem:
			s.tuple = append(s.tuple, n)
		case EdgeTupleRest:
			s.rest = &n
		}
	}
	return s
}

func (vb *vbuilder) object(t GoType, e *plan.ObjectStructurePredicate) []vchild {
	s := childSlots(t)
	if _, ok := deref(t).(*Map); ok {
		return vb.mapObject(t, s, e)
	}
	if _, ok := deref(t).(*Struct); !ok {
		vb.admit("an object structure over a type that stores no object")
		return nil
	}

	var kids []vchild
	for _, pc := range e.Properties {
		f, ok := s.fields[pc.Name]
		if !ok {
			vb.admit("a declared property no field stores")
			continue
		}
		if pres, ok := f.Type.(*Presence); ok && pres.Nullable && !pc.Nullable {
			vb.admit("a null the Go type stores beside a property that does not admit one")
		}
		kids = vb.appendKid(kids, f.Edge, f.Type, pc.Plan)
	}
	for _, pc := range e.Patterns {
		slot, ok := s.patterns[pc.Pattern]
		if !ok {
			vb.admit("a pattern rule no map stores")
			continue
		}
		m, ok := slot.Type.(*Map)
		if !ok {
			vb.admit("a pattern rule stored as something other than a map")
			continue
		}
		kids = vb.appendKid(kids, slot.Edge, m.Elem, pc.Plan)
	}
	if e.Additional != nil && s.additional != nil {
		kids = vb.appendKid(kids, s.additional.Edge, s.additional.Type, *e.Additional)
	}
	return kids
}

func (vb *vbuilder) mapObject(t GoType, s slots, e *plan.ObjectStructurePredicate) []vchild {
	if len(e.Properties) > 0 {
		vb.admit("a declared property inside a map, which holds one element type for every key")
		return nil
	}
	m := deref(t).(*Map)
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
	if p == nil || s.elem == nil {
		vb.admit("an object structure a map cannot be paired with")
		return nil
	}
	return vb.appendKid(nil, s.elem.Edge, s.elem.Type, *p)
}

func (vb *vbuilder) array(t GoType, e *plan.ArrayStructurePredicate) []vchild {
	s := childSlots(t)
	switch deref(t).(type) {
	case *Slice:
		if len(e.Prefix) > 0 {
			vb.admit("a prefix item inside a slice, which holds one element type for every index")
			return nil
		}
		if e.Rest == nil || s.elem == nil {
			return nil
		}
		return vb.appendKid(nil, s.elem.Edge, s.elem.Type, *e.Rest)
	case *Tuple:
		var kids []vchild
		for i, p := range e.Prefix {
			if i >= len(s.tuple) {
				vb.admit("a prefix item no tuple slot stores")
				continue
			}
			kids = vb.appendKid(kids, s.tuple[i].Edge, s.tuple[i].Type, p)
		}
		if e.Rest != nil && s.rest != nil {
			kids = vb.appendKid(kids, s.rest.Edge, s.rest.Type, *e.Rest)
		}
		return kids
	default:
		vb.admit("an array structure over a type that stores no array")
		return nil
	}
}

// appendKid pairs a sub-plan with the type that stores it and hangs the result off e.
//
// The type is passed separately because [EdgePattern] does not carry it: a pattern slot is
// a whole map, and the plan describes one of its values (walk.go says so where the edge is
// built), so what is paired is the element while what is walked through is the map.
func (vb *vbuilder) appendKid(kids []vchild, e Edge, t GoType, p plan.CompilationPlan) []vchild {
	if v := vb.sub(t, p); !v.empty() {
		return append(kids, vchild{edge: e, node: v})
	}
	return kids
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
