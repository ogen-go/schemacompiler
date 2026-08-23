package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// tagFacts is what a union branch is *proven* to say about a candidate discriminator
// property (design §18, discriminator classes 3 and 4). Every field under-claims: a fact
// only ever holds for every instance the branch accepts, so a branch the analysis cannot
// see through degrades to a weaker tier ([plan.TagAsserted] or a warning) instead of
// producing an unsound dispatch (design §24, docs/implementation.md invariant 4).
type tagFacts struct {
	// required means every accepted instance has the property present.
	required bool
	// values, when pinned, is a superset of the values the property may take.
	values []ir.Literal
	pinned bool
	// never means the branch accepts nothing.
	never bool
}

// accepted returns the pinned value set, treating an empty one as unproven: an empty
// superset means the property can take no value at all, which is a contradiction the
// branch analysis is too coarse to attribute reliably.
func (f tagFacts) accepted() ([]ir.Literal, bool) {
	if !f.pinned || len(f.values) == 0 {
		return nil, false
	}
	return f.values, true
}

// and combines the facts of two conjuncts (`allOf` members, `$ref` targets, sibling
// keywords): presence proven by either conjunct holds for the conjunction, and the
// accepted values are the intersection of what each conjunct allows.
func (f tagFacts) and(o tagFacts) tagFacts {
	out := tagFacts{
		required: f.required || o.required,
		never:    f.never || o.never,
	}
	switch {
	case f.pinned && o.pinned:
		out.values, out.pinned = intersectLiterals(f.values, o.values), true
	case f.pinned:
		out.values, out.pinned = f.values, true
	case o.pinned:
		out.values, out.pinned = o.values, true
	}
	return out
}

// orTagFacts combines the facts of a nested combinator's alternatives (issue #45). An
// instance accepted by AnyOf — and by ExactlyOne, which accepts a subset of it — is
// accepted by at least one alternative, so presence must be proven by every alternative
// and the accepted values are the union of what the alternatives allow. One alternative
// that does not pin the property leaves the whole combinator unpinned.
func orTagFacts(subs []tagFacts) tagFacts {
	out := tagFacts{required: true, pinned: true}
	live := 0
	for _, s := range subs {
		if s.never {
			continue
		}
		live++
		out.required = out.required && s.required
		if !s.pinned {
			out.pinned = false
			continue
		}
		if out.pinned {
			out.values = unionLiterals(out.values, s.values)
		}
	}
	if live == 0 {
		return tagFacts{never: true}
	}
	if !out.pinned {
		out.values = nil
	}
	return out
}

// tagFacts derives what e proves about property name, looking through `allOf`, static
// `$ref` targets and nested `anyOf`/`oneOf` combinators (issue #2, issue #45). seen guards
// against reference cycles; a target already visited contributes nothing, which only ever
// loses precision.
func (b *builder) tagFacts(e ir.Expr, name string, seen map[plan.SchemaID]bool) tagFacts {
	c := flattenAll([]ir.Expr{e})
	f := tagFacts{never: c.never, required: requiredProperty(c, name)}
	if values, ok := shapeLiteralValues(c, name); ok {
		f = f.and(tagFacts{values: values, pinned: true})
	}
	for _, r := range c.refs {
		ref, ok := r.(*ir.Ref)
		if !ok || !ref.KindsKnown || seen[ref.Target] {
			// DynamicRef, unresolved static ref or a cycle: not statically knowable.
			continue
		}
		node, ok := b.refTargets()[string(ref.Target)]
		if !ok {
			continue
		}
		if seen == nil {
			seen = make(map[plan.SchemaID]bool)
		}
		seen[ref.Target] = true
		f = f.and(b.tagFacts(ir.Compile(node), name, seen))
	}
	for _, comb := range c.combinators {
		var operands []ir.Expr
		switch v := comb.(type) {
		case *ir.AnyOf:
			operands = v.Operands
		case *ir.ExactlyOne:
			operands = v.Operands
		default:
			continue
		}
		subs := make([]tagFacts, len(operands))
		for i, op := range operands {
			subs[i] = b.tagFacts(op, name, seen)
		}
		f = f.and(orTagFacts(subs))
	}
	return f
}

// shapeLiteralValues intersects every bare const/enum declaration of property name across
// the object shapes of one conjunction. Declarations that are not a bare const/enum are
// skipped: they can only narrow the accepted set further, and the result is consumed as a
// superset obligation.
func shapeLiteralValues(c components, name string) ([]ir.Literal, bool) {
	var acc []ir.Literal
	found := false
	for _, sd := range c.shapes {
		os, ok := sd.(*ir.ObjectShape)
		if !ok {
			continue
		}
		for _, prop := range os.Properties {
			if prop.Name != name {
				continue
			}
			values, ok := literalValues(prop.Schema)
			if !ok {
				continue
			}
			if !found {
				acc, found = values, true
				continue
			}
			acc = intersectLiterals(acc, values)
		}
	}
	return acc, found
}

func unionLiterals(a, b []ir.Literal) []ir.Literal {
	out := append([]ir.Literal{}, a...)
	for _, y := range b {
		if !containsLiteral(out, y) {
			out = append(out, y)
		}
	}
	return out
}
