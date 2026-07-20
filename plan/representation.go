package plan

// Representation is the Go data shape capable of storing accepted values (design §7).
// A backend maps each variant to a concrete Go type.
type Representation interface {
	isRepresentation()
}

// AnyRepresentation accepts any JSON value (e.g. Go any / json.RawMessage).
type AnyRepresentation struct{}

// NeverRepresentation accepts nothing (an unsatisfiable schema).
type NeverRepresentation struct{}

// PrimitiveRepresentation is a single scalar kind, optionally refined for numbers.
type PrimitiveRepresentation struct {
	Kind    JSONKind
	Numeric NumericDomain // meaningful only when Kind == KindNumber
}

// ObjectRepresentation is a struct/map-like shape (design §7, §12).
type ObjectRepresentation struct {
	Fields       map[string]FieldRepresentation
	Additional   Representation // nil means additional properties are not representable as a field
	PatternRules []PatternFieldRepresentation
}

// PatternFieldRepresentation maps a property-name pattern to a value representation.
type PatternFieldRepresentation struct {
	Pattern        string
	Representation Representation
}

// PresenceMode captures whether a field must be present (design §7.1, §12.2).
type PresenceMode uint8

const (
	// PresenceRequired means the property must be present.
	PresenceRequired PresenceMode = iota
	// PresenceOptional means the property may be absent.
	PresenceOptional
)

// FieldRepresentation is one object field. Presence and Nullable are independent
// (design §7.1): absent, present-null, and present-value are three distinct states.
type FieldRepresentation struct {
	Representation Representation
	Presence       PresenceMode
	Nullable       bool
}

// ArrayRepresentation is a tuple prefix plus a homogeneous rest (design §13).
type ArrayRepresentation struct {
	Prefix []Representation
	Rest   Representation // nil means no additional items beyond the prefix
}

// UnionRepresentation is a set of alternatives selected by a DispatchPlan.
type UnionRepresentation struct {
	Alternatives []Representation
}

// RecursiveRepresentation binds a name for a recursive Go type (design §19).
type RecursiveRepresentation struct {
	Name string
	Body Representation
}

// ReferenceRepresentation refers to a named representation (a $ref target or recursion binder).
type ReferenceRepresentation struct {
	Name string
}

func (AnyRepresentation) isRepresentation()       {}
func (NeverRepresentation) isRepresentation()     {}
func (PrimitiveRepresentation) isRepresentation() {}
func (ObjectRepresentation) isRepresentation()    {}
func (ArrayRepresentation) isRepresentation()     {}
func (UnionRepresentation) isRepresentation()     {}
func (RecursiveRepresentation) isRepresentation() {}
func (ReferenceRepresentation) isRepresentation() {}
