package dump

import (
	"fmt"
	"io"
	"sort"

	"github.com/ogen-go/schemacompiler/plan"
)

// Plan pretty-prints a [plan.CompilationPlan] to w: capability, representation,
// validation, dispatch, and resolution, each as an indented tree.
func Plan(w io.Writer, p plan.CompilationPlan) {
	t := &tw{w: w}
	writePlan(t, p, make(map[plan.SchemaID]bool))
}

func writePlan(t *tw, p plan.CompilationPlan, visiting map[plan.SchemaID]bool) {
	t.line("Plan capability=%s", capabilityString(p.Capability))
	t.enter(func() {
		if p.Metadata.Title != "" {
			t.line("title=%q", p.Metadata.Title)
		}

		t.line("Representation")
		t.enter(func() { writeRepresentation(t, p.Representation) })

		t.line("Validation")
		t.enter(func() { writeValidation(t, p.Validation) })

		t.line("Dispatch")
		t.enter(func() { writeDispatch(t, p.Dispatch, visiting) })

		t.line("Resolution")
		t.enter(func() { writeResolution(t, p.Resolution, visiting) })
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
	case plan.PredicateDispatch:
		return "predicate-dispatch"
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

func writeRepresentation(t *tw, r plan.Representation) {
	switch r := r.(type) {
	case nil:
		t.line("<nil>")
	case plan.AnyRepresentation:
		t.line("Any")
	case plan.NeverRepresentation:
		t.line("Never")
	case plan.PrimitiveRepresentation:
		if dom := numericDomainString(r.Numeric); dom != "" {
			t.line("Primitive %s numeric=%s", jsonKindString(r.Kind), dom)
		} else {
			t.line("Primitive %s", jsonKindString(r.Kind))
		}
	case plan.ObjectRepresentation:
		t.line("Object")
		t.enter(func() {
			names := make([]string, 0, len(r.Fields))
			for name := range r.Fields {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				f := r.Fields[name]
				t.line("field %q presence=%s nullable=%v", name, presenceString(f.Presence), f.Nullable)
				t.enter(func() { writeRepresentation(t, f.Representation) })
			}
			if r.Additional != nil {
				t.line("additional")
				t.enter(func() { writeRepresentation(t, r.Additional) })
			}
			for _, pr := range r.PatternRules {
				t.line("patternRule %q", pr.Pattern)
				t.enter(func() { writeRepresentation(t, pr.Representation) })
			}
		})
	case plan.ArrayRepresentation:
		t.line("Array")
		t.enter(func() {
			for i, p := range r.Prefix {
				t.line("prefix[%d]", i)
				t.enter(func() { writeRepresentation(t, p) })
			}
			if r.Rest != nil {
				t.line("rest")
				t.enter(func() { writeRepresentation(t, r.Rest) })
			}
		})
	case plan.UnionRepresentation:
		t.line("Union")
		t.enter(func() {
			for i, alt := range r.Alternatives {
				t.line("alternative[%d]", i)
				t.enter(func() { writeRepresentation(t, alt) })
			}
		})
	case plan.RecursiveRepresentation:
		t.line("Recursive %q", r.Name)
		t.enter(func() { writeRepresentation(t, r.Body) })
	case plan.ReferenceRepresentation:
		t.line("Reference %q", r.Name)
	default:
		t.line("<unknown Representation %T>", r)
	}
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
	if v.Empty() {
		t.line("(empty)")
		return
	}
	for _, gp := range v.Predicates {
		t.line("guard=%s", kindSetString(gp.Applicability))
		t.enter(func() { writePredicateExpr(t, gp.Expression) })
	}
}

func writePredicateExpr(t *tw, e plan.PredicateExpr) {
	switch e := e.(type) {
	case plan.MinLengthPredicate:
		t.line("MinLength %d", e.Value)
	case plan.MaxLengthPredicate:
		t.line("MaxLength %d", e.Value)
	case plan.PatternPredicate:
		t.line("Pattern %q", e.Regex)
	case plan.FormatPredicate:
		t.line("Format %q", e.Format)
	case plan.MinimumPredicate:
		t.line("Minimum %v exclusive=%v", e.Value, e.Exclusive)
	case plan.MaximumPredicate:
		t.line("Maximum %v exclusive=%v", e.Value, e.Exclusive)
	case plan.MultipleOfPredicate:
		t.line("MultipleOf %v", e.Value)
	case plan.MinItemsPredicate:
		t.line("MinItems %d", e.Value)
	case plan.MaxItemsPredicate:
		t.line("MaxItems %d", e.Value)
	case plan.UniqueItemsPredicate:
		t.line("UniqueItems")
	case plan.ContainsCountPredicate:
		t.line("ContainsCount min=%d max=%s", e.Min, uintPtrString(e.Max))
		t.enter(func() { writePlan(t, e.Schema, map[plan.SchemaID]bool{}) })
	case plan.RequiredPredicate:
		t.line("Required %v", e.Properties)
	case plan.MinPropertiesPredicate:
		t.line("MinProperties %d", e.Value)
	case plan.MaxPropertiesPredicate:
		t.line("MaxProperties %d", e.Value)
	case plan.DependentRequiredPredicate:
		t.line("DependentRequired")
		t.enter(func() {
			for _, entry := range e.Entries {
				t.line("%q requires %v", entry.Property, entry.Requires)
			}
		})
	case plan.PropertyNamesPredicate:
		t.line("PropertyNames")
		t.enter(func() { writePlan(t, e.Schema, map[plan.SchemaID]bool{}) })
	default:
		t.line("<unknown PredicateExpr %T>", e)
	}
}

func writeDispatch(t *tw, d plan.DispatchPlan, visiting map[plan.SchemaID]bool) {
	switch d := d.(type) {
	case nil:
		t.line("<nil>")
	case plan.NoDispatch:
		t.line("NoDispatch")
	case plan.KindDispatch:
		t.line("KindDispatch")
		t.enter(func() {
			kinds := make([]plan.JSONKind, 0, len(d.Cases))
			for k := range d.Cases {
				kinds = append(kinds, k)
			}
			sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
			for _, k := range kinds {
				t.line("case %s", jsonKindString(k))
				t.enter(func() { writePlan(t, d.Cases[k], visiting) })
			}
		})
	case plan.LiteralDispatch:
		t.line("LiteralDispatch")
		t.enter(func() { writeLiteralCases(t, d.Cases, visiting) })
	case plan.PropertyDispatch:
		t.line("PropertyDispatch property=%q", d.Property)
		t.enter(func() { writeLiteralCases(t, d.Cases, visiting) })
	case plan.PresenceDispatch:
		t.line("PresenceDispatch property=%q", d.Property)
		t.enter(func() {
			t.line("present")
			t.enter(func() { writePlan(t, d.Present, visiting) })
			t.line("absent")
			t.enter(func() { writePlan(t, d.Absent, visiting) })
		})
	case plan.PredicateCountDispatch:
		t.line("PredicateCountDispatch min=%d max=%d", d.Minimum, d.Maximum)
		t.enter(func() {
			for i, br := range d.Branches {
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
		t.line("case %#v", c.Value)
		t.enter(func() { writePlan(t, c.Plan, visiting) })
	}
}

func writeResolution(t *tw, r plan.ResolutionPlan, visiting map[plan.SchemaID]bool) {
	switch r := r.(type) {
	case nil:
		t.line("<nil>")
	case plan.FullyResolved:
		t.line("FullyResolved")
	case plan.StaticReferenceGraph:
		t.line("StaticReferenceGraph")
		t.enter(func() { writeDefinitions(t, r.Definitions, visiting) })
	case plan.DynamicReferenceGraph:
		t.line("DynamicReferenceGraph")
		t.enter(func() {
			t.line("static definitions")
			t.enter(func() { writeDefinitions(t, r.StaticDefinitions, visiting) })

			anchors := make([]string, 0, len(r.DynamicAnchors))
			for a := range r.DynamicAnchors {
				anchors = append(anchors, a)
			}
			sort.Strings(anchors)
			for _, a := range anchors {
				t.line("dynamicAnchor %q -> %v", a, r.DynamicAnchors[a])
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
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

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
