package planner

import (
	"encoding/json"
	"strings"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// declaredDispatchCases builds the cases of a union carrying an explicit OpenAPI
// `discriminator` (issue #17). It reports false when a branch yields no value on the
// declared property, or when two branches share one, in which case the caller falls back
// to structural inference.
func (b *builder) declaredDispatchCases(d *ir.Discriminator, branchExprs []ir.Expr) ([]discCase, bool) {
	if d.PropertyName == "" {
		return nil, false
	}
	var cases []discCase
	seen := newValueSet(len(branchExprs))
	for _, be := range branchExprs {
		lits := b.declaredBranchValues(d, be)
		if len(lits) == 0 {
			return nil, false
		}
		for _, lit := range lits {
			if !seen.add(lit) {
				return nil, false
			}
			cases = append(cases, discCase{Value: lit.Value, Raw: lit.Raw, Expr: be})
		}
	}
	return cases, true
}

// declaredBranchValues returns every value selecting be, in preference order: the
// `mapping` entries denoting the branch (several are allowed: aliases of one schema), the
// branch's own const on the declared property, then the implicit mapping — the component
// name of the branch's `$ref` (OpenAPI 3.1 §4.8.25.1).
func (b *builder) declaredBranchValues(d *ir.Discriminator, be ir.Expr) []ir.Literal {
	targets := branchRefTargets(be)

	var mapped []ir.Literal
	for _, m := range d.Mapping {
		for _, t := range targets {
			if refMatchesTarget(m.Ref, t) {
				mapped = append(mapped, stringLiteral(m.Value))
				break
			}
		}
	}
	if len(mapped) > 0 {
		return mapped
	}

	c := b.flattenThroughRefs(be, nil)
	if c.never {
		return nil
	}
	if _, lit, ok := requiredConstProperty(c, d.PropertyName); ok {
		return []ir.Literal{lit}
	}

	for _, t := range targets {
		if name := componentName(string(t)); name != "" {
			return []ir.Literal{stringLiteral(name)}
		}
	}
	return nil
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
