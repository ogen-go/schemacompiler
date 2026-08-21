package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// declaredDispatchCases builds the cases of a union carrying an explicit OpenAPI
// `discriminator` (issue #17). The returned reason is empty on success; otherwise it
// explains why the declaration cannot drive dispatch, and the caller warns and falls back
// to structural inference.
func (b *builder) declaredDispatchCases(d *ir.Discriminator, branchExprs []ir.Expr) (cases []discCase, reason string) {
	if d.PropertyName == "" {
		return nil, "discriminator has no propertyName"
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
			return nil, fmt.Sprintf("discriminator mapping %q => %q resolves to no branch of this union", m.Value, m.Ref)
		}
	}

	seen := newValueSet(len(branchExprs))
	for i, be := range branchExprs {
		c := b.flattenThroughRefs(be, nil)
		if c.never || !declaresProperty(c, d.PropertyName) {
			return nil, fmt.Sprintf("discriminator property %q is not declared by every branch", d.PropertyName)
		}
		values := mapped[i]
		if len(values) == 0 {
			values = declaredBranchValues(c, targets[i], d.PropertyName)
		}
		if len(values) == 0 {
			return nil, fmt.Sprintf("discriminator property %q has no value on every branch", d.PropertyName)
		}
		for _, lit := range values {
			if !seen.add(lit) {
				return nil, fmt.Sprintf("discriminator value %s selects more than one branch", literalString(lit))
			}
			cases = append(cases, discCase{Value: lit.Value, Raw: lit.Raw, Expr: be})
		}
	}
	return cases, ""
}

// declaredBranchValues returns the values selecting a branch no mapping entry named: its
// own const on the declared property, else the implicit mapping — the component name of
// its `$ref` (OpenAPI 3.1 §4.8.25.1).
func declaredBranchValues(c components, targets []plan.SchemaID, property string) []ir.Literal {
	if _, lit, ok := requiredConstProperty(c, property); ok {
		return []ir.Literal{lit}
	}
	for _, t := range targets {
		if name := componentName(string(t)); name != "" {
			return []ir.Literal{stringLiteral(name)}
		}
	}
	return nil
}

// declaresProperty reports whether a branch declares the property the discriminator
// switches on, as a required name or as an object property. OpenAPI requires it on every
// branch; dispatching on a property no branch carries could never select one at runtime.
func declaresProperty(c components, name string) bool {
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
	for _, sd := range c.shapes {
		os, ok := sd.(ir.ObjectShape)
		if !ok {
			continue
		}
		for _, prop := range os.Properties {
			if prop.Name == name {
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
