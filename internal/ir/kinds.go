package ir

import "github.com/ogen-go/schemacompiler/plan"

// Kind inference (design §6). Type-specific keywords do not restrict kinds: a bare
// predicate or shape accepts every kind, and only its guard fires for the applicable
// kind at runtime (design §3, §6.1).

// Kinds implements [Expr].
func (Any) Kinds() plan.KindSet { return plan.SetAny }

// Kinds implements [Expr].
func (Never) Kinds() plan.KindSet { return 0 }

// Kinds implements [Expr].
func (e Kinds) Kinds() plan.KindSet { return e.Set }

// Kinds implements [Expr].
func (e Literal) Kinds() plan.KindSet { return literalKind(e.Value) }

// Kinds implements [Expr].
func (Predicate) Kinds() plan.KindSet { return plan.SetAny }

// Kinds implements [Expr].
func (Shape) Kinds() plan.KindSet { return plan.SetAny }

// Kinds returns the intersection of operand kinds (design §6.1); empty All == Any.
func (e All) Kinds() plan.KindSet {
	set := plan.SetAny
	for _, o := range e.Operands {
		set &= o.Kinds()
	}
	return set
}

// Kinds returns the union of operand kinds (design §6.1); empty AnyOf == Never.
func (e AnyOf) Kinds() plan.KindSet {
	var set plan.KindSet
	for _, o := range e.Operands {
		set |= o.Kinds()
	}
	return set
}

// Kinds returns the union of branch kinds: exact-one is a cardinality constraint on
// top, not a kind restriction.
func (e ExactlyOne) Kinds() plan.KindSet {
	var set plan.KindSet
	for _, o := range e.Operands {
		set |= o.Kinds()
	}
	return set
}

// Kinds returns the exact kind complement only when the operand is a pure kind
// restriction (design §6) — one that accepts or rejects whole kinds and nothing finer.
// Otherwise a conservative SetAny is required, since a residual predicate/shape only
// ever narrows within a kind, never across it.
func (e Not) Kinds() plan.KindSet {
	if set, ok := pureKindRestriction(e.Operand); ok {
		return plan.SetAny &^ set
	}
	return plan.SetAny
}

// pureKindRestriction reports whether e restricts the accepted JSON kinds and nothing
// finer within a kind (e.g. no integer-only numeric refinement), and if so returns the
// accepted set. Any, Never, and Kinds (with no numeric refinement) qualify directly; a
// single-operand All wrapping a pure kind restriction also qualifies, since it adds no
// further constraint.
func pureKindRestriction(e Expr) (plan.KindSet, bool) {
	switch e := e.(type) {
	case Any:
		return plan.SetAny, true
	case Never:
		return 0, true
	case Kinds:
		if e.Numeric == plan.AnyNumber {
			return e.Set, true
		}
		return 0, false
	case All:
		if len(e.Operands) == 1 {
			return pureKindRestriction(e.Operands[0])
		}
		return 0, false
	default:
		return 0, false
	}
}

// Kinds returns the resolved target's kind summary when known, else SetAny (design §6).
// This is what lets a oneOf/anyOf of bare $ref branches be proven kind-disjoint.
func (e Ref) Kinds() plan.KindSet {
	if e.KindsKnown {
		return e.TargetKinds
	}
	return plan.SetAny
}

// Kinds implements [Expr].
func (DynamicRef) Kinds() plan.KindSet { return plan.SetAny }

// Kinds implements [Expr].
func (e Annotated) Kinds() plan.KindSet { return e.Expr.Kinds() }

// literalKind returns the JSON kind of a decoded JSON value (json.Unmarshal form).
func literalKind(v any) plan.KindSet {
	switch v.(type) {
	case nil:
		return plan.SetNull
	case bool:
		return plan.SetBoolean
	case float64, int, int64:
		return plan.SetNumber
	case string:
		return plan.SetString
	case []any:
		return plan.SetArray
	case map[string]any:
		return plan.SetObject
	default:
		// Unknown decoded form; be conservative.
		return plan.SetAny
	}
}
