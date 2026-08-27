package dump

import (
	"fmt"
	"io"
	"slices"

	"github.com/ogen-go/schemacompiler/plan"
)

// Plan pretty-prints a [plan.CompilationPlan] to w: capability, representation,
// validation, dispatch, and resolution, each as an indented tree.
func Plan(w io.Writer, p plan.CompilationPlan) {
	t := &tw{w: w}
	writePlan(t, p, make(map[plan.SchemaID]bool))
}

func writePlan(t *tw, p plan.CompilationPlan, visiting map[plan.SchemaID]bool) {
	var g struct {
		Representation plan.Representation
		Validation     plan.ValidationPlan
		Dispatch       plan.DispatchPlan
		Resolution     plan.ResolutionPlan
		Capability     plan.CapabilityLevel
		Metadata       plan.Metadata
	} = p

	t.line("Plan capability=%s", capabilityString(g.Capability))
	t.enter(func() {
		writeMetadata(t, g.Metadata)

		t.line("Representation")
		t.enter(func() { writeRepresentation(t, g.Representation, visiting) })

		t.line("Validation")
		t.enter(func() { writeValidation(t, g.Validation) })

		t.line("Dispatch")
		t.enter(func() { writeDispatch(t, g.Dispatch, visiting) })

		t.line("Resolution")
		t.enter(func() { writeResolution(t, g.Resolution, visiting) })
	})
}

func capabilityString(c plan.CapabilityLevel) string {
	switch c {
	case plan.DirectGoType:
		return "direct-go-type"
	case plan.GoTypeWithValidation:
		return "go-type-with-validation"
	case plan.StaticDispatch:
		return "static-dispatch"
	case plan.RawEvaluation:
		return "raw-evaluation"
	case plan.EvaluationStateValidation:
		return "evaluation-state-validation"
	case plan.DynamicSchemaResolution:
		return "dynamic-schema-resolution"
	case plan.Unsupported:
		return "unsupported"
	default:
		return fmt.Sprintf("capability(%d)", c)
	}
}

func writeRepresentation(t *tw, r plan.Representation, visiting map[plan.SchemaID]bool) {
	switch r := r.(type) {
	case nil:
		t.line("<nil>")
	case *plan.AnyRepresentation:
		var g struct{} = *r
		_ = g
		t.line("Any")
	case *plan.NeverRepresentation:
		var g struct{} = *r
		_ = g
		t.line("Never")
	case *plan.PrimitiveRepresentation:
		var g struct {
			Kind    plan.JSONKind
			Numeric plan.NumericDomain
			Format  string
		} = *r
		line := "Primitive " + jsonKindString(g.Kind)
		if dom := numericDomainString(g.Numeric); dom != "" {
			line += " numeric=" + dom
		}
		if g.Format != "" {
			line += fmt.Sprintf(" format=%q", g.Format)
		}
		t.line("%s", line)
	case *plan.ObjectRepresentation:
		var g struct {
			Fields       []plan.FieldRepresentation
			Additional   *plan.CompilationPlan
			PatternRules []plan.PatternFieldRepresentation
		} = *r
		t.line("Object")
		t.enter(func() {
			// Source order, not sorted: Fields is ordered now (issue #89), and sorting
			// would hide the very thing a reader dumps a plan to see.
			for _, f := range g.Fields {
				writeField(t, f, visiting)
			}
			if g.Additional != nil {
				t.line("additional")
				t.enter(func() { writePlan(t, *g.Additional, visiting) })
			}
			for _, pr := range g.PatternRules {
				writePatternField(t, pr, visiting)
			}
		})
	case *plan.ArrayRepresentation:
		var g struct {
			Prefix []plan.ItemRepresentation
			Rest   plan.ItemRepresentation
		} = *r
		t.line("Array")
		t.enter(func() {
			for i, p := range g.Prefix {
				writeItem(t, fmt.Sprintf("prefix[%d]", i), p, visiting)
			}
			if g.Rest.Plan.Representation != nil {
				writeItem(t, "rest", g.Rest, visiting)
			}
		})
	case *plan.UnionRepresentation:
		var g struct {
			Alternatives []plan.Representation
		} = *r
		t.line("Union")
		t.enter(func() {
			for i, alt := range g.Alternatives {
				t.line("alternative[%d]", i)
				t.enter(func() { writeRepresentation(t, alt, visiting) })
			}
		})
	case *plan.ReferenceRepresentation:
		var g struct {
			Name string
		} = *r
		t.line("Reference %q", g.Name)
	default:
		t.line("<unknown Representation %T>", r)
	}
}

func writeField(t *tw, f plan.FieldRepresentation, visiting map[plan.SchemaID]bool) {
	var g struct {
		Name     string
		Plan     plan.CompilationPlan
		Presence plan.PresenceMode
		Nullable bool
		Metadata plan.Metadata
	} = f
	t.line("field %q presence=%s nullable=%v", g.Name, presenceString(g.Presence), g.Nullable)
	t.enter(func() {
		writeMetadata(t, g.Metadata)
		writePlan(t, g.Plan, visiting)
	})
}

func writePatternField(t *tw, p plan.PatternFieldRepresentation, visiting map[plan.SchemaID]bool) {
	var g struct {
		Pattern  string
		Plan     plan.CompilationPlan
		Metadata plan.Metadata
	} = p
	t.line("patternRule %q", g.Pattern)
	t.enter(func() {
		writeMetadata(t, g.Metadata)
		writePlan(t, g.Plan, visiting)
	})
}

func writeItem(t *tw, label string, i plan.ItemRepresentation, visiting map[plan.SchemaID]bool) {
	var g struct {
		Plan     plan.CompilationPlan
		Presence plan.PresenceMode
		Metadata plan.Metadata
	} = i
	t.line("%s presence=%s", label, presenceString(g.Presence))
	t.enter(func() {
		writeMetadata(t, g.Metadata)
		writePlan(t, g.Plan, visiting)
	})
}

func presenceString(p plan.PresenceMode) string {
	switch p {
	case plan.PresenceRequired:
		return "required"
	case plan.PresenceOptional:
		return "optional"
	default:
		return fmt.Sprintf("presence(%d)", p)
	}
}

func writeValidation(t *tw, v plan.ValidationPlan) {
	var g struct {
		Predicates []plan.GuardedPredicate
	} = v
	if len(g.Predicates) == 0 {
		t.line("(empty)")
		return
	}
	for _, gp := range g.Predicates {
		var p struct {
			Applicability plan.KindSet
			Assert        bool
			Expression    plan.PredicateExpr
		} = gp
		if p.Assert && p.Expression == nil {
			t.line("assert=%s", kindSetString(p.Applicability))
			continue
		}
		t.line("guard=%s", kindSetString(p.Applicability))
		t.enter(func() { writePredicateExpr(t, p.Expression) })
	}
}

func writePredicateExpr(t *tw, e plan.PredicateExpr) {
	switch e := e.(type) {
	case *plan.MinLengthPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MinLength %d", g.Value)
	case *plan.MaxLengthPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MaxLength %d", g.Value)
	case *plan.PatternPredicate:
		var g struct{ Regex string } = *e
		t.line("Pattern %q", g.Regex)
	case *plan.FormatPredicate:
		var g struct{ Format string } = *e
		t.line("Format %q", g.Format)
	case *plan.MinimumPredicate:
		var g struct {
			Value     float64
			Exclusive bool
		} = *e
		t.line("Minimum %v exclusive=%v", g.Value, g.Exclusive)
	case *plan.MaximumPredicate:
		var g struct {
			Value     float64
			Exclusive bool
		} = *e
		t.line("Maximum %v exclusive=%v", g.Value, g.Exclusive)
	case *plan.NumericDomainPredicate:
		var g struct{ Domain plan.NumericDomain } = *e
		t.line("NumericDomain %v", g.Domain)
	case *plan.ReferencePredicate:
		var g struct{ Name string } = *e
		t.line("Reference %q", g.Name)
	case *plan.ObjectStructurePredicate:
		var g struct {
			Properties []plan.PropertyCheck
			Patterns   []plan.PatternCheck
			Additional *plan.CompilationPlan
		} = *e
		t.line("ObjectStructure")
		t.enter(func() {
			for _, pc := range g.Properties {
				t.line("property %q presence=%v nullable=%v", pc.Name, pc.Presence, pc.Nullable)
				t.enter(func() { writePlan(t, pc.Plan, map[plan.SchemaID]bool{}) })
			}
			for _, pc := range g.Patterns {
				t.line("pattern %q", pc.Pattern)
				t.enter(func() { writePlan(t, pc.Plan, map[plan.SchemaID]bool{}) })
			}
			if g.Additional != nil {
				t.line("additional")
				t.enter(func() { writePlan(t, *g.Additional, map[plan.SchemaID]bool{}) })
			}
		})
	case *plan.ArrayStructurePredicate:
		var g struct {
			Prefix []plan.CompilationPlan
			Rest   *plan.CompilationPlan
		} = *e
		t.line("ArrayStructure")
		t.enter(func() {
			for i, sub := range g.Prefix {
				t.line("prefix %d", i)
				t.enter(func() { writePlan(t, sub, map[plan.SchemaID]bool{}) })
			}
			if g.Rest != nil {
				t.line("rest")
				t.enter(func() { writePlan(t, *g.Rest, map[plan.SchemaID]bool{}) })
			}
		})
	case *plan.MultipleOfPredicate:
		var g struct{ Value float64 } = *e
		t.line("MultipleOf %v", g.Value)
	case *plan.MinItemsPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MinItems %d", g.Value)
	case *plan.MaxItemsPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MaxItems %d", g.Value)
	case *plan.UniqueItemsPredicate:
		var g struct{} = *e
		_ = g
		t.line("UniqueItems")
	case *plan.ContainsCountPredicate:
		var g struct {
			Schema plan.CompilationPlan
			Min    uint64
			Max    *uint64
		} = *e
		t.line("ContainsCount min=%d max=%s", g.Min, uintPtrString(g.Max))
		t.enter(func() { writePlan(t, g.Schema, map[plan.SchemaID]bool{}) })
	case *plan.NegationPredicate:
		var g struct{ Schema plan.CompilationPlan } = *e
		t.line("Negation")
		t.enter(func() { writePlan(t, g.Schema, map[plan.SchemaID]bool{}) })
	case *plan.RequiredPredicate:
		var g struct{ Properties []string } = *e
		t.line("Required %v", g.Properties)
	case *plan.MinPropertiesPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MinProperties %d", g.Value)
	case *plan.MaxPropertiesPredicate:
		var g struct{ Value uint64 } = *e
		t.line("MaxProperties %d", g.Value)
	case *plan.DependentRequiredPredicate:
		var g struct{ Entries []plan.DependentRequiredEntry } = *e
		t.line("DependentRequired")
		t.enter(func() {
			for _, entry := range g.Entries {
				var d struct {
					Property string
					Requires []string
				} = entry
				t.line("%q requires %v", d.Property, d.Requires)
			}
		})
	case *plan.PropertyNamesPredicate:
		var g struct{ Schema plan.CompilationPlan } = *e
		t.line("PropertyNames")
		t.enter(func() { writePlan(t, g.Schema, map[plan.SchemaID]bool{}) })
	case *plan.ShapePredicate:
		var g struct{ Schema plan.CompilationPlan } = *e
		t.line("Shape")
		t.enter(func() { writePlan(t, g.Schema, map[plan.SchemaID]bool{}) })
	default:
		t.line("<unknown PredicateExpr %T>", e)
	}
}

func writeDispatch(t *tw, d plan.DispatchPlan, visiting map[plan.SchemaID]bool) {
	switch d := d.(type) {
	case nil:
		t.line("<nil>")
	case *plan.NoDispatch:
		var g struct{} = *d
		_ = g
		t.line("NoDispatch")
	case *plan.KindDispatch:
		var g struct {
			Cases map[plan.JSONKind]plan.CompilationPlan
		} = *d
		t.line("KindDispatch")
		t.enter(func() {
			kinds := make([]plan.JSONKind, 0, len(g.Cases))
			for k := range g.Cases {
				kinds = append(kinds, k)
			}
			slices.Sort(kinds)
			for _, k := range kinds {
				t.line("case %s", jsonKindString(k))
				t.enter(func() { writePlan(t, g.Cases[k], visiting) })
			}
		})
	case *plan.LiteralDispatch:
		var g struct {
			Cases []plan.LiteralCase
		} = *d
		t.line("LiteralDispatch")
		t.enter(func() { writeLiteralCases(t, g.Cases, visiting) })
	case *plan.PropertyDispatch:
		var g struct {
			Property string
			Cases    []plan.LiteralCase
			Tag      plan.TagSource
		} = *d
		if g.Tag == plan.TagDeclared {
			t.line("PropertyDispatch property=%q declared", g.Property)
		} else {
			t.line("PropertyDispatch property=%q", g.Property)
		}
		t.enter(func() { writeLiteralCases(t, g.Cases, visiting) })
	case *plan.PresenceDispatch:
		var g struct {
			Property string
			Present  plan.CompilationPlan
			Absent   plan.CompilationPlan
		} = *d
		t.line("PresenceDispatch property=%q", g.Property)
		t.enter(func() {
			t.line("present")
			t.enter(func() { writePlan(t, g.Present, visiting) })
			t.line("absent")
			t.enter(func() { writePlan(t, g.Absent, visiting) })
		})
	case *plan.PredicateCountDispatch:
		var g struct {
			Branches []plan.CompilationPlan
			Minimum  int
			Maximum  int
		} = *d
		t.line("PredicateCountDispatch min=%d max=%d", g.Minimum, g.Maximum)
		t.enter(func() {
			for i, br := range g.Branches {
				t.line("branch[%d]", i)
				t.enter(func() { writePlan(t, br, visiting) })
			}
		})
	default:
		t.line("<unknown DispatchPlan %T>", d)
	}
}

func writeLiteralCases(t *tw, cases []plan.LiteralCase, visiting map[plan.SchemaID]bool) {
	for _, c := range cases {
		var g struct {
			Value any
			Raw   []byte
			Plan  plan.CompilationPlan
		} = c
		t.line("case %#v", g.Value)
		t.enter(func() { writePlan(t, g.Plan, visiting) })
	}
}

func writeResolution(t *tw, r plan.ResolutionPlan, visiting map[plan.SchemaID]bool) {
	switch r := r.(type) {
	case nil:
		t.line("<nil>")
	case *plan.FullyResolved:
		var g struct{} = *r
		_ = g
		t.line("FullyResolved")
	case *plan.StaticReferenceGraph:
		var g struct {
			Definitions map[plan.SchemaID]plan.CompilationPlan
		} = *r
		t.line("StaticReferenceGraph")
		t.enter(func() { writeDefinitions(t, g.Definitions, visiting) })
	case *plan.DynamicReferenceGraph:
		var g struct {
			StaticDefinitions map[plan.SchemaID]plan.CompilationPlan
			DynamicAnchors    map[string][]plan.SchemaID
		} = *r
		t.line("DynamicReferenceGraph")
		t.enter(func() {
			t.line("static definitions")
			t.enter(func() { writeDefinitions(t, g.StaticDefinitions, visiting) })

			anchors := make([]string, 0, len(g.DynamicAnchors))
			for a := range g.DynamicAnchors {
				anchors = append(anchors, a)
			}
			slices.Sort(anchors)
			for _, a := range anchors {
				t.line("dynamicAnchor %q -> %v", a, g.DynamicAnchors[a])
			}
		})
	default:
		t.line("<unknown ResolutionPlan %T>", r)
	}
}

// writeDefinitions prints each definition sorted by SchemaID, guarding against infinite
// recursion when definitions reference each other (or themselves) via a visited set.
func writeDefinitions(t *tw, defs map[plan.SchemaID]plan.CompilationPlan, visiting map[plan.SchemaID]bool) {
	ids := make([]plan.SchemaID, 0, len(defs))
	for id := range defs {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		t.line("definition %q", id)
		if visiting[id] {
			t.enter(func() { t.line("<cycle>") })
			continue
		}
		visiting[id] = true
		t.enter(func() { writePlan(t, defs[id], visiting) })
		delete(visiting, id)
	}
}
