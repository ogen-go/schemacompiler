package ir

// This file defines the concrete [ShapeDetail] variants: structural constraints that
// carry compiled sub-expressions (design §5, §12, §13). A [Shape] never asserts a kind
// by itself (design §3, §6.1) — it only fires when the runtime instance happens to be
// an object or array.

// PropertyExpr is one `properties` entry: Name maps to its compiled sub-schema.
type PropertyExpr struct {
	Name   string
	Schema Expr
}

// PatternPropertyExpr is one `patternProperties` entry.
type PatternPropertyExpr struct {
	Pattern string
	Schema  Expr
}

// ObjectShape is the object-guarded structural contribution of `properties`,
// `patternProperties`, `additionalProperties`, and `unevaluatedProperties` (design
// §12.1, §12.3, §12.4, §14). AdditionalProperties/UnevaluatedProperties are nil when
// the corresponding keyword is absent.
type ObjectShape struct {
	Properties            []PropertyExpr
	PatternProperties     []PatternPropertyExpr
	AdditionalProperties  Expr
	UnevaluatedProperties Expr
}

// ArrayShape is the array-guarded structural contribution of `prefixItems`, `items`,
// and `unevaluatedItems` (design §13.1, §13.2, §14). Items/UnevaluatedItems are nil
// when the corresponding keyword is absent.
type ArrayShape struct {
	PrefixItems      []Expr
	Items            Expr
	UnevaluatedItems Expr
}

func (ObjectShape) isShapeDetail() {}
func (ArrayShape) isShapeDetail()  {}
