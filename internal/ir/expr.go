// Package ir is the semantic intermediate representation of a JSON Schema (design §5).
//
// It preserves exact JSON Schema semantics: the distinction among All (allOf), AnyOf
// (anyOf), ExactlyOne (oneOf), and Not (not) is explicit and is never flattened before
// disjointness is proved. Type-specific keywords are guarded predicates, not type
// assertions (design §3).
//
// ir depends only on plan for shared value types (KindSet, NumericDomain); it must not
// import the parser (libopenapi) — the frontend adapter produces the schema AST this
// package compiles from.
package ir

import "github.com/ogen-go/schemacompiler/plan"

// Expr is a semantic expression: a predicate over the universe of JSON values.
type Expr interface {
	isExpr()
	// Kinds returns the abstract set of JSON kinds this expression can accept (design §6).
	Kinds() plan.KindSet
}

// Any accepts every JSON value (schema true).
type Any struct{}

// Never accepts nothing (schema false).
type Never struct{}

// Kinds restricts the accepted JSON kinds (from type), optionally refining numbers.
type Kinds struct {
	Set     plan.KindSet
	Numeric plan.NumericDomain
}

// Literal is an exact JSON value (const), contributing both representation and equality.
type Literal struct {
	Value any
}

// Predicate is a residual, kind-guarded check (minLength, minimum, pattern, required, ...).
// Guard restricts the kinds the predicate applies to; other kinds pass vacuously (design §3).
type Predicate struct {
	Guard plan.KindSet
	// Keyword and payload are filled in by the semantic compiler; a Kind tag plus typed
	// fields are added in phase 2. Kept opaque here so the contract compiles.
	Detail PredicateDetail
}

// PredicateDetail carries the concrete keyword and its operands (defined in phase 2).
type PredicateDetail interface {
	isPredicateDetail()
}

// Shape describes object/array structural constraints (properties, items, ...) that are
// not yet lowered to a representation (design §5, §12, §13). Defined in phase 2.
type Shape struct {
	Detail ShapeDetail
}

// ShapeDetail carries the concrete structural constraint (defined in phase 2).
type ShapeDetail interface {
	isShapeDetail()
}

// All is intersection (allOf, and sibling-keyword conjunction). Empty All == Any.
type All struct{ Operands []Expr }

// AnyOf is union (anyOf, enum). Empty AnyOf == Never.
type AnyOf struct{ Operands []Expr }

// ExactlyOne is oneOf: exactly one operand matches. Not a union until proven disjoint.
type ExactlyOne struct{ Operands []Expr }

// Not is complement.
type Not struct{ Operand Expr }

// Ref is a static reference to another schema resource (design §10.1).
type Ref struct {
	Target plan.SchemaID
	// TargetKinds summarizes the JSON kinds the resolved target accepts (design §6),
	// letting normalization and dispatch see through the reference to prove oneOf/anyOf
	// branch disjointness. It is meaningful only when KindsKnown is set; an unresolved or
	// unanalyzed ref reports [plan.SetAny] (conservative). Computed cycle-safely in
	// refkinds.go: a reference cycle yields SetAny rather than looping.
	TargetKinds plan.KindSet
	KindsKnown  bool
}

// DynamicRef is a scope-sensitive reference resolved during validation (design §10.2).
type DynamicRef struct {
	Anchor string
}

// Annotated attaches evaluation annotations (evaluated properties/items) needed for
// unevaluatedProperties/unevaluatedItems (design §14). Detail defined in phase 2/4.
type Annotated struct {
	Expr        Expr
	Annotations EvaluationAnnotations
}

// EvaluationAnnotations records which locations a successful branch evaluated (design §14).
type EvaluationAnnotations struct {
	// Filled in phase 4; kept as a marker so the contract compiles.
}

func (Any) isExpr()        {}
func (Never) isExpr()      {}
func (Kinds) isExpr()      {}
func (Literal) isExpr()    {}
func (Predicate) isExpr()  {}
func (Shape) isExpr()      {}
func (All) isExpr()        {}
func (AnyOf) isExpr()      {}
func (ExactlyOne) isExpr() {}
func (Not) isExpr()        {}
func (Ref) isExpr()        {}
func (DynamicRef) isExpr() {}
func (Annotated) isExpr()  {}
