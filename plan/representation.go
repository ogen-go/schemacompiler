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

// PrimitiveRepresentation is a single scalar kind, optionally refined for numbers and
// by a `format` (design §7).
type PrimitiveRepresentation struct {
	Kind    JSONKind
	Numeric NumericDomain // meaningful only when Kind == KindNumber
	// Format is the `format` annotation applying to this kind, as written. The compiler
	// assigns it no meaning: choosing a Go type for a name is a backend decision. The
	// matching [FormatPredicate] stays in the validation plan regardless, so a backend
	// that ignores Format still validates (design §24 invariant 4).
	Format string
}

// ObjectRepresentation is a struct/map-like shape (design §7, §12).
type ObjectRepresentation struct {
	// Fields are the declared properties in source order, the order a backend should
	// generate struct fields in (issue #89). Names are unique within one object.
	Fields []FieldRepresentation
	// Additional is the plan for every property no field and no pattern rule covers.
	// nil means additional properties are not representable as a field.
	Additional *CompilationPlan
	// PatternRules cover the property names no Fields entry declares. A declared field
	// owns its name outright: every `patternProperties` schema whose pattern matches it
	// is already intersected into that field's own plan (design §12.3), so a value stored
	// in a field is never also checked against a rule here.
	PatternRules []PatternFieldRepresentation
}

// PatternFieldRepresentation maps a property-name pattern to the plan of the values
// stored under it.
type PatternFieldRepresentation struct {
	Pattern  string
	Plan     CompilationPlan
	Metadata Metadata
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
//
// Plan is the field value's whole compilation plan, not just its shape: a sub-schema's
// validation and dispatch belong to the property they were written on, and a slot that
// could only hold a [Representation] would drop them (issue #68).
type FieldRepresentation struct {
	// Name is the property name this field stores.
	Name     string
	Plan     CompilationPlan
	Presence PresenceMode
	Nullable bool
	Metadata Metadata
}

// ItemRepresentation is one array position: the plan of the values stored there plus the
// annotations of the sub-schema it came from.
type ItemRepresentation struct {
	Plan     CompilationPlan
	Metadata Metadata
}

// ArrayRepresentation is a tuple prefix plus a homogeneous rest (design §13).
type ArrayRepresentation struct {
	Prefix []ItemRepresentation
	// Rest describes items beyond the prefix; a nil Rest.Plan.Representation means
	// there are none.
	Rest ItemRepresentation
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
