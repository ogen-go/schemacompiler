package gogen

import (
	"github.com/ogen-go/schemacompiler/plan"
)

// GoType is the Go data shape a [plan.Representation] lowers to.
//
// It is data, never source text (docs/backend.md §4): a renderer that reads it can be
// syntactically wrong, which a compiler catches, but not semantically wrong, which
// nothing catches. That is what lets ogen render its own source without drifting from us.
//
// Variants implement this on a pointer receiver, so only *T satisfies it, matching the
// [plan] convention (issue #133).
type GoType interface {
	isGoType()
}

// Named is a declared type. Use sites hold the same *Named the declaration does, so the
// lowered types form a real graph and a recursive schema is a cycle of Go pointers rather
// than a name to look up.
type Named struct {
	// ID is the schema this type was lowered from, and Name the Go identifier
	// [Assign] gave it.
	ID   plan.SchemaID
	Name string
	// Underlying is the type Name is defined as.
	Underlying GoType
	// Recursive reports that this type is a member of a cycle of Go-inline storage, so
	// every direct reference to it is a [Pointer]. It is a property of the type, not of
	// any one reference: which edge to break is not canonical, which node to break at
	// is (docs/backend.md §7).
	Recursive bool
	Metadata  plan.Metadata
}

// PrimitiveKind is a Go scalar.
type PrimitiveKind uint8

// The Go scalars a [plan.PrimitiveRepresentation] lowers to.
const (
	PrimitiveString PrimitiveKind = iota
	PrimitiveBool
	PrimitiveInt
	PrimitiveFloat
	// PrimitiveNull is the type of a schema accepting only JSON null, which holds no
	// information. Only a renderer decides what to spell it.
	PrimitiveNull
)

var primitiveNames = [...]string{
	PrimitiveString: "string",
	PrimitiveBool:   "bool",
	PrimitiveInt:    "int",
	PrimitiveFloat:  "float",
	PrimitiveNull:   "null",
}

func (k PrimitiveKind) String() string {
	if int(k) >= len(primitiveNames) {
		return "primitive-kind(?)"
	}
	return primitiveNames[k]
}

// Primitive is a scalar. Format is the schema's `format` verbatim, uninterpreted here as
// it is in the plan: choosing a Go type for a format name is a rendering decision, and
// the matching predicate stays in the validation plan either way (design §24 invariant 4).
type Primitive struct {
	Kind   PrimitiveKind
	Format string
}

// Any accepts every JSON value.
type Any struct{}

// Never accepts no value. It reaches a slot only where the schema is unsatisfiable there:
// `additionalProperties: false` closes an object rather than producing this.
type Never struct{}

// Slice is a homogeneous array.
type Slice struct {
	Elem GoType
}

// Tuple is an array with a fixed prefix. Rest is the type of the items past the prefix,
// nil when the schema admits none.
type Tuple struct {
	Elems []GoType
	Rest  GoType
}

// Map is an object stored by property name. Pattern is the `patternProperties` regex whose
// values it holds, empty when it holds `additionalProperties` instead.
//
// The pattern is kept because it is what routes a key to this map at runtime, and because
// nothing downstream can re-derive it: the plan states each rule positionally, and a Go
// map type on its own says only that the keys are strings.
type Map struct {
	Elem    GoType
	Pattern string
}

// Struct is an object with declared fields.
//
// Patterns is one map per `patternProperties` rule, in the plan's order, each keeping its
// own element type. A key is routed to *every* map whose pattern it matches, not the first
// — JSON Schema conjoins the matching rules (design §12.3) — and to Additional only when it
// matches none. Additional is the type of those, nil when the object is closed.
//
// A declared field is never routed here: the planner has already intersected every matching
// pattern schema into that field's own plan, so its slot is exact.
type Struct struct {
	Fields     []Field
	Patterns   []*Map
	Additional GoType
}

// Field is one struct field.
type Field struct {
	// Name is the Go field name; JSON is the property name it stores.
	Name     string
	JSON     string
	Type     GoType
	Metadata plan.Metadata
}

// Presence wraps a type in the [opt] three-state storage: Optional alone is opt.Opt,
// Nullable alone is opt.Nullable, and both is opt.OptNullable. Neither is never built —
// the wrapper would carry nothing.
type Presence struct {
	Elem     GoType
	Optional bool
	Nullable bool
}

// Interface is a sum of alternatives, one of which holds the value.
type Interface struct {
	Variants []GoType
}

// Pointer is indirection, added only by the recursion pass to break a cycle of inline
// storage.
type Pointer struct {
	Elem GoType
}

func (*Named) isGoType()     {}
func (*Primitive) isGoType() {}
func (*Any) isGoType()       {}
func (*Never) isGoType()     {}
func (*Slice) isGoType()     {}
func (*Tuple) isGoType()     {}
func (*Map) isGoType()       {}
func (*Struct) isGoType()    {}
func (*Presence) isGoType()  {}
func (*Interface) isGoType() {}
func (*Pointer) isGoType()   {}
