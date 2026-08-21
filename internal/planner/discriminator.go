package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// declaredDispatchCases builds the cases of a union carrying an explicit OpenAPI
// `discriminator` (issue #17), together with the tag source recording how much of the
// dispatch is proven (issue #31).
//
// A declaration is not by itself a disjointness proof (design §18, §15.3), but OAS 3.0.3
// line 2717 explicitly permits a discriminator to "act as a 'hint' to shortcut validation
// and selection of the matching schema", so the declaration is trusted where the spec
// allows it and the plan records that it was trusted rather than proven:
//
//   - [plan.TagDeclared] when every branch requires the property, pins it to a const/enum
//     and its cases cover every value it accepts — the same obligation the inferred path
//     discharges, so the branches are provably disjoint.
//   - [plan.TagAsserted] when every branch requires the property but the values come only
//     from `mapping`. Dispatch is the declared hint; disjointness is assumed.
//
// A non-empty reason means the declaration cannot drive dispatch at all — the property is
// not required (OAS 3.0.3 line 2354 makes that mandatory), no value selects a branch, or
// one value selects several — and the caller warns and falls back to structural inference
// and, ultimately, to predicate-count dispatch (design §20.6, §22).
func (b *builder) declaredDispatchCases(d *ir.Discriminator, branchExprs []ir.Expr) (cases []discCase, tag plan.TagSource, reason string) {
	if d.PropertyName == "" {
		return nil, 0, "discriminator has no propertyName"
	}

	targets := make([][]plan.SchemaID, len(branchExprs))
	for i, be := range branchExprs {
		targets[i] = branchRefTargets(be)
	}

	mapped := make([][]ir.Literal, len(branchExprs))
	for _, m := range d.Mapping {
		matched := false
		for i, ts := range targets {
			if refMatchesAny(m.Ref, ts) {
				mapped[i] = append(mapped[i], stringLiteral(m.Value))
				matched = true
			}
		}
		if !matched {
			return nil, 0, fmt.Sprintf("discriminator mapping %q => %q resolves to no branch of this union", m.Value, m.Ref)
		}
	}

	tag = plan.TagDeclared
	seen := newValueSet(len(branchExprs))
	for i, be := range branchExprs {
		c := b.flattenThroughRefs(be, nil)
		if c.never {
			return nil, 0, "discriminator union has an uninhabited branch"
		}
		if !requiredProperty(c, d.PropertyName) {
			return nil, 0, fmt.Sprintf(
				"discriminator property %q is not required by every branch, which OAS 3.0.3 line 2354 mandates",
				d.PropertyName)
		}
		accepted, proven := requiredLiteralValues(c, d.PropertyName)
		values := mapped[i]
		switch {
		case len(values) == 0 && !proven:
			// The implicit mapping (OpenAPI 3.1 §4.8.25.1) names the component, which
			// constrains nothing in the instance and so cannot select a branch (design
			// §18); without a mapping entry or a branch const there is no value to
			// switch on.
			return nil, 0, fmt.Sprintf("discriminator property %q has no value selecting every branch", d.PropertyName)
		case len(values) == 0:
			// The branch's own const/enum: the component name coincides with it exactly
			// when the branch constrains the property to that literal.
			values = accepted
		case !proven:
			tag = plan.TagAsserted
		default:
			if _, covered := coversAll(values, accepted); !covered {
				tag = plan.TagAsserted
			}
		}
		for _, lit := range values {
			if !seen.add(lit) {
				return nil, 0, fmt.Sprintf("discriminator value %s selects more than one branch", literalString(lit))
			}
			cases = append(cases, discCase{Value: lit.Value, Raw: lit.Raw, Expr: be})
		}
	}
	return cases, tag, ""
}

// requiredLiteralValues returns the values a branch accepts on a required property,
// proving the branch is observable from that property alone (design §18, discriminator
// classes 3 and 4). Declarations of the property that are not a bare const/enum are
// skipped: they can only narrow the accepted set further, and the result is consumed as a
// superset obligation.
func requiredLiteralValues(c components, name string) ([]ir.Literal, bool) {
	if !requiredProperty(c, name) {
		return nil, false
	}
	var acc []ir.Literal
	found := false
	for _, sd := range c.shapes {
		os, ok := sd.(ir.ObjectShape)
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
	if !found || len(acc) == 0 {
		return nil, false
	}
	return acc, true
}

// literalValues reports whether e restricts its instance to a finite set of literals: a
// bare const, or the AnyOf of literals an enum desugars to.
func literalValues(e ir.Expr) ([]ir.Literal, bool) {
	if lit, ok := extractLiteral(e); ok {
		return []ir.Literal{lit}, true
	}
	c := flattenAll([]ir.Expr{e})
	if c.never || c.literal != nil || len(c.shapes) > 0 || len(c.predicates) > 0 ||
		len(c.nots) > 0 || len(c.refs) > 0 || len(c.combinators) != 1 {
		return nil, false
	}
	anyOf, ok := c.combinators[0].(ir.AnyOf)
	if !ok || len(anyOf.Operands) == 0 {
		return nil, false
	}
	out := make([]ir.Literal, 0, len(anyOf.Operands))
	for _, op := range anyOf.Operands {
		lit, ok := op.(ir.Literal)
		if !ok {
			return nil, false
		}
		out = append(out, lit)
	}
	return out, true
}

func intersectLiterals(a, b []ir.Literal) []ir.Literal {
	var out []ir.Literal
	for _, x := range a {
		for _, y := range b {
			if x.Equal(y) {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

// coversAll reports whether every literal of want appears in have, returning the first
// uncovered one otherwise.
func coversAll(have, want []ir.Literal) (ir.Literal, bool) {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h.Equal(w) {
				found = true
				break
			}
		}
		if !found {
			return w, false
		}
	}
	return ir.Literal{}, true
}

func requiredProperty(c components, name string) bool {
	for _, p := range c.predicates {
		rd, ok := p.Detail.(ir.RequiredDetail)
		if !ok {
			continue
		}
		for _, prop := range rd.Properties {
			if prop == name {
				return true
			}
		}
	}
	return false
}

// branchRefTargets returns the targets of the static `$ref`s a branch is built from.
func branchRefTargets(be ir.Expr) []plan.SchemaID {
	var out []plan.SchemaID
	for _, r := range flattenAll([]ir.Expr{be}).refs {
		if ref, ok := r.(ir.Ref); ok {
			out = append(out, ref.Target)
		}
	}
	return out
}

func refMatchesAny(ref string, targets []plan.SchemaID) bool {
	for _, t := range targets {
		if refMatchesTarget(ref, t) {
			return true
		}
	}
	return false
}

// refMatchesTarget reports whether a `mapping` reference denotes target, which is a
// resolved schema's document pointer (or the raw reference of an unresolved one). A
// mapping value is either a reference ("#/components/schemas/Cat") or a bare component
// name ("Cat").
func refMatchesTarget(ref string, target plan.SchemaID) bool {
	t := string(target)
	if ref == t || strings.TrimPrefix(ref, "#") == t {
		return true
	}
	if strings.ContainsAny(ref, "/#") {
		return false
	}
	return componentName(t) == ref
}

func componentName(target string) string {
	i := strings.LastIndex(target, "/")
	if i < 0 {
		return ""
	}
	return target[i+1:]
}

func stringLiteral(s string) ir.Literal {
	raw, err := json.Marshal(s)
	if err != nil {
		return ir.Literal{Value: s}
	}
	return ir.Literal{Value: s, Raw: raw}
}

func literalString(l ir.Literal) string {
	if l.Raw != nil {
		return string(l.Raw)
	}
	return fmt.Sprintf("%v", l.Value)
}
