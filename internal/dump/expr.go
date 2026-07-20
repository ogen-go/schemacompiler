package dump

import (
	"fmt"
	"io"

	"github.com/ogen-go/schemacompiler/internal/ir"
)

// Expr pretty-prints a semantic IR tree to w, one node per line, indented two spaces per
// nesting level.
func Expr(w io.Writer, e ir.Expr) {
	t := &tw{w: w}
	writeExpr(t, e)
}

func writeExpr(t *tw, e ir.Expr) {
	switch e := e.(type) {
	case ir.Any:
		t.line("Any")
	case ir.Never:
		t.line("Never")
	case ir.Kinds:
		if dom := numericDomainString(e.Numeric); dom != "" {
			t.line("Kinds %s numeric=%s", kindSetString(e.Set), dom)
		} else {
			t.line("Kinds %s", kindSetString(e.Set))
		}
	case ir.Literal:
		t.line("Literal %#v", e.Value)
	case ir.Predicate:
		t.line("Predicate guard=%s", kindSetString(e.Guard))
		t.enter(func() { writePredicateDetail(t, e.Detail) })
	case ir.Shape:
		t.line("Shape")
		t.enter(func() { writeShapeDetail(t, e.Detail) })
	case ir.All:
		t.line("All")
		t.enter(func() {
			for _, o := range e.Operands {
				writeExpr(t, o)
			}
		})
	case ir.AnyOf:
		t.line("AnyOf")
		t.enter(func() {
			for _, o := range e.Operands {
				writeExpr(t, o)
			}
		})
	case ir.ExactlyOne:
		t.line("ExactlyOne")
		t.enter(func() {
			for _, o := range e.Operands {
				writeExpr(t, o)
			}
		})
	case ir.Not:
		t.line("Not")
		t.enter(func() { writeExpr(t, e.Operand) })
	case ir.Ref:
		if e.KindsKnown {
			t.line("Ref target=%q kinds=%s", e.Target, kindSetString(e.TargetKinds))
		} else {
			t.line("Ref target=%q kinds=unknown", e.Target)
		}
	case ir.DynamicRef:
		t.line("DynamicRef anchor=%q", e.Anchor)
	case ir.Annotated:
		t.line("Annotated")
		t.enter(func() { writeExpr(t, e.Expr) })
	default:
		t.line("<unknown Expr %T>", e)
	}
}

func writePredicateDetail(t *tw, d ir.PredicateDetail) {
	switch d := d.(type) {
	case ir.MinLengthDetail:
		t.line("MinLength %d", d.Value)
	case ir.MaxLengthDetail:
		t.line("MaxLength %d", d.Value)
	case ir.PatternDetail:
		t.line("Pattern %q", d.Regex)
	case ir.FormatDetail:
		t.line("Format %q", d.Format)
	case ir.MinimumDetail:
		t.line("Minimum %v", d.Value)
	case ir.MaximumDetail:
		t.line("Maximum %v", d.Value)
	case ir.ExclusiveMinimumDetail:
		t.line("ExclusiveMinimum %v", d.Value)
	case ir.ExclusiveMaximumDetail:
		t.line("ExclusiveMaximum %v", d.Value)
	case ir.MultipleOfDetail:
		t.line("MultipleOf %v", d.Value)
	case ir.MinItemsDetail:
		t.line("MinItems %d", d.Value)
	case ir.MaxItemsDetail:
		t.line("MaxItems %d", d.Value)
	case ir.UniqueItemsDetail:
		t.line("UniqueItems")
	case ir.ContainsDetail:
		t.line("Contains min=%s max=%s", uintPtrString(d.Min), uintPtrString(d.Max))
		t.enter(func() { writeExpr(t, d.Schema) })
	case ir.RequiredDetail:
		t.line("Required %v", d.Properties)
	case ir.MinPropertiesDetail:
		t.line("MinProperties %d", d.Value)
	case ir.MaxPropertiesDetail:
		t.line("MaxProperties %d", d.Value)
	case ir.DependentRequiredDetail:
		t.line("DependentRequired")
		t.enter(func() {
			for _, entry := range d.Entries {
				t.line("%q requires %v", entry.Property, entry.Requires)
			}
		})
	case ir.PropertyNamesDetail:
		t.line("PropertyNames")
		t.enter(func() { writeExpr(t, d.Schema) })
	default:
		t.line("<unknown PredicateDetail %T>", d)
	}
}

func writeShapeDetail(t *tw, d ir.ShapeDetail) {
	switch d := d.(type) {
	case ir.ObjectShape:
		t.line("ObjectShape")
		t.enter(func() {
			for _, p := range d.Properties {
				t.line("property %q", p.Name)
				t.enter(func() { writeExpr(t, p.Schema) })
			}
			for _, p := range d.PatternProperties {
				t.line("patternProperty %q", p.Pattern)
				t.enter(func() { writeExpr(t, p.Schema) })
			}
			if d.AdditionalProperties != nil {
				t.line("additionalProperties")
				t.enter(func() { writeExpr(t, d.AdditionalProperties) })
			}
			if d.UnevaluatedProperties != nil {
				t.line("unevaluatedProperties")
				t.enter(func() { writeExpr(t, d.UnevaluatedProperties) })
			}
		})
	case ir.ArrayShape:
		t.line("ArrayShape")
		t.enter(func() {
			for i, p := range d.PrefixItems {
				t.line("prefixItems[%d]", i)
				t.enter(func() { writeExpr(t, p) })
			}
			if d.Items != nil {
				t.line("items")
				t.enter(func() { writeExpr(t, d.Items) })
			}
			if d.UnevaluatedItems != nil {
				t.line("unevaluatedItems")
				t.enter(func() { writeExpr(t, d.UnevaluatedItems) })
			}
		})
	default:
		t.line("<unknown ShapeDetail %T>", d)
	}
}

func uintPtrString(p *uint64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *p)
}
