package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/plan"
)

// This file intersects the object/array shape siblings of one All (design §11.5: allOf is
// an unordered intersection).
//
// The intersection is per property name and per array index, not per keyword. `properties`
// and the element schema of `items` do intersect across siblings, but `additionalProperties`
// and `items` are scoped to the schema object that declared them: `additionalProperties`
// applies to names its *own* object's `properties`/`patternProperties` do not match, and
// `items` applies to indexes past its *own* object's `prefixItems` (design §12.3, §12.4,
// §13.2). Neither sees what an applicator declared, so a shape's own keywords have to be
// resolved against each other before the siblings are conjoined (issue #94):
//
//	{"allOf": [{"properties": {"foo": {}}}], "additionalProperties": false}
//
// rejects {"foo": 1}, because the outer `additionalProperties` does not see `foo`.

// mergedObject is every ObjectShape sibling of an All resolved into one object
// representation: one constraint per declared name, the pattern rules, and the plan for
// everything else.
type mergedObject struct {
	order        []string
	fields       map[string]ir.Expr
	metadata     map[string]plan.Metadata
	patternRules []ir.PatternPropertyExpr
	// additional is the constraint on a name no sibling declares and no pattern matches:
	// every sibling's own `additionalProperties`. It is nil when no sibling has one.
	additional  ir.Expr
	unevaluated bool
}

func (b *builder) mergeObjectShapes(shapes []ir.ShapeDetail, path string) mergedObject {
	objects := objectShapes(shapes)
	m := mergedObject{
		fields:   make(map[string]ir.Expr),
		metadata: make(map[string]plan.Metadata),
	}

	var additional []ir.Expr
	for _, os := range objects {
		for _, p := range os.Properties {
			if _, seen := m.fields[p.Name]; !seen {
				m.fields[p.Name] = nil
				m.order = append(m.order, p.Name)
			}
			// The same property may be declared by several conjoined siblings; only one
			// annotation set can survive, so the last sibling that carries any wins.
			if !metadataEmpty(p.Metadata) {
				m.metadata[p.Name] = p.Metadata
			}
		}
		if os.AdditionalProperties != nil {
			additional = append(additional, os.AdditionalProperties)
		}
		if os.UnevaluatedProperties != nil {
			m.unevaluated = true
		}
	}
	if len(additional) > 0 {
		m.additional = intersectExprs(additional)
	}

	for _, name := range m.order {
		var operands []ir.Expr
		for _, os := range objects {
			operands = append(operands, b.constraintsFor(name, os, path)...)
		}
		expr := intersectExprs(operands)
		if expr == nil {
			expr = &ir.Any{}
		}
		m.fields[name] = expr
	}
	m.patternRules = b.mergePatternRules(objects, path)
	return m
}

// mergePatternRules carries each sibling's `patternProperties` entry over unchanged except
// for the other siblings' `additionalProperties`: a name this rule matches is not matched
// by the sibling that declared them, so that sibling's `additionalProperties` applies to it
// too.
//
// A sibling that has patterns of its own is left out of that fold, since whether one of its
// patterns matches the names this rule matches is not decidable from the two patterns. The
// constraint is dropped rather than assumed, so the plan accepts more instead of rejecting
// instances the schema accepts (design §24).
func (b *builder) mergePatternRules(objects []ir.ObjectShape, path string) []ir.PatternPropertyExpr {
	var out []ir.PatternPropertyExpr
	for i, os := range objects {
		if len(os.PatternProperties) == 0 {
			continue
		}
		var extra []ir.Expr
		for j, other := range objects {
			if i == j || other.AdditionalProperties == nil {
				continue
			}
			if len(other.PatternProperties) > 0 {
				b.dropped = true
				b.diag(path, plan.DiagnosticUnenforced, plan.SeverityWarning, "two conjoined schema objects both declare patternProperties; "+
					"the additionalProperties of one is not intersected into the pattern rules of the other")
				continue
			}
			extra = append(extra, other.AdditionalProperties)
		}
		for _, pp := range os.PatternProperties {
			rule := pp
			if len(extra) > 0 {
				rule.Schema = intersectExprs(append([]ir.Expr{pp.Schema}, extra...))
			}
			out = append(out, rule)
		}
	}
	return out
}

// mergedArray is every ArrayShape sibling of an All resolved into one array
// representation: one constraint per tuple index and one for the rest.
type mergedArray struct {
	prefix      []ir.ItemExpr
	items       ir.ItemExpr
	unevaluated bool
}

func mergeArrayShapes(shapes []ir.ShapeDetail) mergedArray {
	arrays := arrayShapes(shapes)
	var m mergedArray

	width := 0
	var items []ir.Expr
	for _, as := range arrays {
		width = max(width, len(as.PrefixItems))
		if as.Items.Schema != nil {
			items = append(items, as.Items.Schema)
			if !metadataEmpty(as.Items.Metadata) {
				m.items.Metadata = as.Items.Metadata
			}
		}
		if as.UnevaluatedItems != nil {
			m.unevaluated = true
		}
	}
	if len(items) > 0 {
		m.items.Schema = intersectExprs(items)
	}

	m.prefix = make([]ir.ItemExpr, width)
	for i := range m.prefix {
		var operands []ir.Expr
		var md plan.Metadata
		for _, as := range arrays {
			// Past a sibling's own prefix, its `items` is what covers this index.
			it := as.Items
			if i < len(as.PrefixItems) {
				it = as.PrefixItems[i]
			}
			if it.Schema == nil {
				continue
			}
			operands = append(operands, it.Schema)
			if !metadataEmpty(it.Metadata) {
				md = it.Metadata
			}
		}
		m.prefix[i] = ir.ItemExpr{Schema: intersectExprs(operands), Metadata: md}
	}
	return m
}

func objectShapes(shapes []ir.ShapeDetail) []ir.ObjectShape {
	var out []ir.ObjectShape
	for _, sd := range shapes {
		if os, ok := sd.(*ir.ObjectShape); ok {
			out = append(out, *os)
		}
	}
	return out
}

func arrayShapes(shapes []ir.ShapeDetail) []ir.ArrayShape {
	var out []ir.ArrayShape
	for _, sd := range shapes {
		if as, ok := sd.(*ir.ArrayShape); ok {
			out = append(out, *as)
		}
	}
	return out
}

// intersectExprs conjoins operands the planner composed itself, so the result has to be
// re-normalized before a sub-builder sees it.
func intersectExprs(operands []ir.Expr) ir.Expr {
	switch len(operands) {
	case 0:
		return nil
	case 1:
		return operands[0]
	default:
		return norm.Normalize(&ir.All{Operands: operands}, renormalizeBudget)
	}
}
