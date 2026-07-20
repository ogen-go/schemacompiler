package ir

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// Compile converts a [frontend.Node] into a semantic [Expr] (design §21.1). A schema's
// sibling keywords compile to the [All] (conjunction) of their per-keyword expressions.
// Type-specific keywords are always emitted as kind-guarded predicates/shapes here
// (design §3); dropping a guard made redundant by an enclosing `type` is normalization's
// job (phase 3), not this compiler's.
func Compile(n *frontend.Node) Expr {
	if n == nil {
		// A missing sub-schema (e.g. unset `items`) behaves as the empty schema `true`.
		return Any{}
	}
	if n.Always != nil {
		if *n.Always {
			return Any{}
		}
		return Never{}
	}

	var siblings []Expr

	if n.Ref != "" {
		siblings = append(siblings, Ref{
			Target:      refTarget(n),
			TargetKinds: refTargetKinds(n),
			KindsKnown:  n.Resolved != nil,
		})
	}
	if n.DynamicRef != "" {
		siblings = append(siblings, DynamicRef{Anchor: n.DynamicRef})
	}

	if n.HasType {
		numeric := plan.AnyNumber
		if n.IntegerType {
			numeric = plan.IntegerOnly
		}
		siblings = append(siblings, Kinds{Set: plan.KindSet(n.Types), Numeric: numeric})
	}

	if n.Const != nil {
		siblings = append(siblings, Literal{Value: n.Const.Decoded})
	}
	if len(n.Enum) > 0 {
		operands := make([]Expr, len(n.Enum))
		for i, v := range n.Enum {
			operands[i] = Literal{Value: v.Decoded}
		}
		siblings = append(siblings, AnyOf{Operands: operands})
	}

	if len(n.AllOf) > 0 {
		siblings = append(siblings, All{Operands: compileAll(n.AllOf)})
	}
	if len(n.AnyOf) > 0 {
		siblings = append(siblings, AnyOf{Operands: compileAll(n.AnyOf)})
	}
	if len(n.OneOf) > 0 {
		siblings = append(siblings, ExactlyOne{Operands: compileAll(n.OneOf)})
	}
	if n.Not != nil {
		siblings = append(siblings, Not{Operand: Compile(n.Not)})
	}
	if n.If != nil {
		siblings = append(siblings, compileIfThenElse(n))
	}

	siblings = append(siblings, compileStringKeywords(n)...)
	siblings = append(siblings, compileNumericKeywords(n)...)
	siblings = append(siblings, compileArrayKeywords(n)...)
	siblings = append(siblings, compileObjectKeywords(n)...)

	return All{Operands: siblings}
}

// compileAll compiles each sub-schema in order.
func compileAll(nodes []*frontend.Node) []Expr {
	operands := make([]Expr, len(nodes))
	for i, sub := range nodes {
		operands[i] = Compile(sub)
	}
	return operands
}

// compileIfThenElse desugars `if`/`then`/`else` per design §11.9:
//
//	AnyOf(All(P, T), All(Not(P), E))
func compileIfThenElse(n *frontend.Node) Expr {
	p := Compile(n.If)
	t := Compile(n.Then)
	e := Compile(n.Else)
	return AnyOf{Operands: []Expr{
		All{Operands: []Expr{p, t}},
		All{Operands: []Expr{Not{Operand: p}, e}},
	}}
}

// refTarget derives a deterministic [plan.SchemaID] for a `$ref`. A resolved static ref
// is keyed by its target's own document pointer (stable regardless of how it was
// spelled); an unresolved ref falls back to the raw reference string.
func refTarget(n *frontend.Node) plan.SchemaID {
	if n.Resolved != nil {
		return plan.SchemaID(n.Resolved.Pointer)
	}
	return plan.SchemaID(n.Ref)
}

func compileStringKeywords(n *frontend.Node) []Expr {
	var out []Expr
	guard := func(detail PredicateDetail) {
		out = append(out, Predicate{Guard: plan.SetString, Detail: detail})
	}
	if n.MinLength != nil {
		guard(MinLengthDetail{Value: *n.MinLength})
	}
	if n.MaxLength != nil {
		guard(MaxLengthDetail{Value: *n.MaxLength})
	}
	if n.Pattern != nil {
		guard(PatternDetail{Regex: *n.Pattern})
	}
	if n.Format != "" {
		guard(FormatDetail{Format: n.Format})
	}
	return out
}

func compileNumericKeywords(n *frontend.Node) []Expr {
	var out []Expr
	guard := func(detail PredicateDetail) {
		out = append(out, Predicate{Guard: plan.SetNumber, Detail: detail})
	}
	if n.Minimum != nil {
		guard(MinimumDetail{Value: *n.Minimum})
	}
	if n.Maximum != nil {
		guard(MaximumDetail{Value: *n.Maximum})
	}
	if n.ExclusiveMinimum != nil {
		guard(ExclusiveMinimumDetail{Value: *n.ExclusiveMinimum})
	}
	if n.ExclusiveMaximum != nil {
		guard(ExclusiveMaximumDetail{Value: *n.ExclusiveMaximum})
	}
	if n.MultipleOf != nil {
		guard(MultipleOfDetail{Value: *n.MultipleOf})
	}
	return out
}

func compileArrayKeywords(n *frontend.Node) []Expr {
	var out []Expr
	guard := func(detail PredicateDetail) {
		out = append(out, Predicate{Guard: plan.SetArray, Detail: detail})
	}

	if len(n.PrefixItems) > 0 || n.Items != nil || n.UnevaluatedItems != nil {
		shape := ArrayShape{PrefixItems: compileAll(n.PrefixItems)}
		if n.Items != nil {
			shape.Items = Compile(n.Items)
		}
		if n.UnevaluatedItems != nil {
			shape.UnevaluatedItems = Compile(n.UnevaluatedItems)
		}
		out = append(out, Shape{Detail: shape})
	}

	if n.MinItems != nil {
		guard(MinItemsDetail{Value: *n.MinItems})
	}
	if n.MaxItems != nil {
		guard(MaxItemsDetail{Value: *n.MaxItems})
	}
	if n.UniqueItems {
		guard(UniqueItemsDetail{})
	}
	if n.Contains != nil || n.MinContains != nil || n.MaxContains != nil {
		guard(ContainsDetail{
			Schema: Compile(n.Contains),
			Min:    n.MinContains,
			Max:    n.MaxContains,
		})
	}
	return out
}

func compileObjectKeywords(n *frontend.Node) []Expr {
	var out []Expr
	guard := func(detail PredicateDetail) {
		out = append(out, Predicate{Guard: plan.SetObject, Detail: detail})
	}

	if len(n.Properties) > 0 || len(n.PatternProperties) > 0 ||
		n.AdditionalProperties != nil || n.UnevaluatedProperties != nil {
		shape := ObjectShape{}
		for _, p := range n.Properties {
			shape.Properties = append(shape.Properties, PropertyExpr{
				Name:   p.Name,
				Schema: Compile(p.Schema),
			})
		}
		for _, p := range n.PatternProperties {
			shape.PatternProperties = append(shape.PatternProperties, PatternPropertyExpr{
				Pattern: p.Name,
				Schema:  Compile(p.Schema),
			})
		}
		if n.AdditionalProperties != nil {
			shape.AdditionalProperties = Compile(n.AdditionalProperties)
		}
		if n.UnevaluatedProperties != nil {
			shape.UnevaluatedProperties = Compile(n.UnevaluatedProperties)
		}
		out = append(out, Shape{Detail: shape})
	}

	if len(n.Required) > 0 {
		guard(RequiredDetail{Properties: n.Required})
	}
	if n.MinProperties != nil {
		guard(MinPropertiesDetail{Value: *n.MinProperties})
	}
	if n.MaxProperties != nil {
		guard(MaxPropertiesDetail{Value: *n.MaxProperties})
	}
	if len(n.DependentRequired) > 0 {
		entries := make([]DependentRequiredEntry, len(n.DependentRequired))
		for i, d := range n.DependentRequired {
			entries[i] = DependentRequiredEntry{Property: d.Property, Requires: d.Requires}
		}
		guard(DependentRequiredDetail{Entries: entries})
	}
	if n.PropertyNames != nil {
		guard(PropertyNamesDetail{Schema: Compile(n.PropertyNames)})
	}
	for _, d := range n.DependentSchemas {
		out = append(out, compileDependentSchema(d))
	}
	return out
}

// compileDependentSchema desugars one `dependentSchemas` entry (design §12.7):
//
//	Has(p) => C(S)  ≡  Not(Has(p)) or All(Has(p), C(S))
func compileDependentSchema(d frontend.NamedSchema) Expr {
	has := Predicate{Guard: plan.SetObject, Detail: RequiredDetail{Properties: []string{d.Name}}}
	return AnyOf{Operands: []Expr{
		Not{Operand: has},
		All{Operands: []Expr{has, Compile(d.Schema)}},
	}}
}
