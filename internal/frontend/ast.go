package frontend

import "encoding/json"

// This file defines the presence-normalized internal AST (Node) that ir compiles from.
// It mirrors JSON Schema Draft 2020-12 but with a uniform presence model: a keyword is
// represented by a nil pointer / empty slice when absent, so ir can distinguish
// "minimum: 0" from "minimum unset" (design §3). Sub-schemas are *Node; object-keyed
// keywords keep declaration order for deterministic downstream output.
//
// Phase 1 may extend Node with resolution back-pointers and provenance, but existing
// fields are a stable contract for phases 2-4.

// Value is a JSON literal (const/enum/default element). Decoded is the json.Unmarshal
// form (float64 for numbers, etc.); Raw preserves the exact source bytes for precision-
// sensitive uses.
type Value struct {
	Decoded any
	Raw     json.RawMessage
}

// Node is one schema. A boolean schema (`true`/`false`) is represented by a non-nil
// Always; all other fields are then zero.
type Node struct {
	// Always is non-nil for a boolean schema: *true accepts everything, *false nothing.
	Always *bool

	// Identity / references (design §10). Ref and DynamicRef hold the raw reference
	// strings; Phase 1 resolution fills Resolved / the Registry.
	ID            string
	Ref           string
	DynamicRef    string
	Anchor        string
	DynamicAnchor string
	Defs          []NamedSchema

	// Resolved is the target Node for a static Ref, populated by Phase 1 resolution.
	// nil for non-reference nodes or unresolved dynamic refs.
	Resolved *Node

	// Pointer is the JSON Pointer to this node from its document root (for diagnostics).
	Pointer string

	// type. HasType reports whether the `type` keyword was present; Types is the kind set
	// it asserted. Integer is recorded via IntegerType.
	HasType     bool
	Types       KindSet
	IntegerType bool // type included "integer"

	// const / enum.
	Const *Value
	Enum  []Value

	// Numeric assertions (nil = unset).
	Minimum          *float64
	Maximum          *float64
	ExclusiveMinimum *float64
	ExclusiveMaximum *float64
	MultipleOf       *float64

	// String assertions.
	MinLength *uint64
	MaxLength *uint64
	Pattern   *string
	Format    string

	// Array assertions.
	PrefixItems []*Node
	Items       *Node // 2020-12 `items`: applies after the prefix
	Contains    *Node
	MinContains *uint64
	MaxContains *uint64
	MinItems    *uint64
	MaxItems    *uint64
	UniqueItems bool

	// Object assertions (ordered).
	Properties            []NamedSchema
	PatternProperties     []NamedSchema
	AdditionalProperties  *Node // nil = unset; a `false` becomes an always-false Node
	PropertyNames         *Node
	Required              []string
	DependentRequired     []DependentRequired
	DependentSchemas      []NamedSchema
	MinProperties         *uint64
	MaxProperties         *uint64
	UnevaluatedProperties *Node
	UnevaluatedItems      *Node

	// Applicators.
	AllOf []*Node
	AnyOf []*Node
	OneOf []*Node
	Not   *Node
	If    *Node
	Then  *Node
	Else  *Node

	// Metadata (non-semantic, propagated to plan.Metadata).
	Title       string
	Description string
	Default     *Value
	Deprecated  bool
	ReadOnly    bool
	WriteOnly   bool
	Examples    []Value
}

// NamedSchema is one entry of an order-preserving schema map ($defs, properties,
// patternProperties, dependentSchemas).
type NamedSchema struct {
	Name   string
	Schema *Node
}

// DependentRequired is one `dependentRequired` entry: presence of Property requires Requires.
type DependentRequired struct {
	Property string
	Requires []string
}

// KindSet duplicates plan.KindSet's bit layout for the frontend's type keyword so the
// AST does not depend on plan. Phase 1 converts between them.
type KindSet uint8

// JSON kind bits for [KindSet]. The layout matches plan.KindSet.
const (
	KindNull KindSet = 1 << iota
	KindBoolean
	KindNumber
	KindString
	KindArray
	KindObject
)
