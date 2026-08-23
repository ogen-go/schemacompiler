package plan

// CompilationPlan is the analyzed result for one schema (design §4). It separates the
// four independent compiler concerns so a backend can lower each on its own terms.
type CompilationPlan struct {
	// Representation is the Go data shape (design §7).
	Representation Representation
	// Validation is the residual, kind-guarded predicate (design §8).
	Validation ValidationPlan
	// Dispatch selects among known alternatives at runtime (design §9).
	Dispatch DispatchPlan
	// Resolution describes reference resolution (design §10).
	Resolution ResolutionPlan
	// Capability is the highest-cost construct this plan needs (design §4.1, §22).
	Capability CapabilityLevel
	// Metadata carries schema annotations useful to a backend (title, description, ...).
	Metadata Metadata
}

// Metadata holds non-semantic schema annotations propagated for code generation.
type Metadata struct {
	Title       string
	Description string
	Deprecated  bool
	ReadOnly    bool
	WriteOnly   bool
	// Default and Examples are raw JSON, as written in the source document.
	Default  []byte
	Examples [][]byte
	XML      *XMLMetadata
	// Extensions holds every `x-*` keyword of the schema, decoded to Go-native values
	// and otherwise uninterpreted: the compiler assigns no meaning to any key.
	Extensions map[string]any
}

// XMLMetadata is the OpenAPI `xml` object of a schema.
type XMLMetadata struct {
	Name      string
	Namespace string
	Prefix    string
	Attribute bool
	Wrapped   bool
}

// Diagnostic explains why a stronger conversion was not possible (design §25).
type Diagnostic struct {
	// Pointer is the JSON Pointer to the offending schema location, when known.
	Pointer string
	// Position is the source location of that schema, when the parser retained one.
	Position Position
	// Kind says what the diagnostic means for the plan's accepted set; Severity says
	// how loudly to report it. They are not the same axis: an ignored keyword and an
	// unresolved $ref are both errors and both [DiagnosticUnenforced], while a
	// conflicting format and an unused discriminator differ in severity and are both
	// [DiagnosticAdvisory].
	Kind     DiagnosticKind
	Severity Severity
	Message  string
}

// DiagnosticKind says what a diagnostic reports about the relation between the schema's
// accepted set and the plan's. It is the machine-readable half of §24.1's rule that a
// plan accepting more than its schema must say which construct it failed to enforce:
// message text cannot be keyed on, and Severity conflates cost with incompleteness.
type DiagnosticKind uint8

const (
	// DiagnosticUnclassified is the zero value, carried by no diagnostic the compiler
	// emits. It exists so that a Diagnostic built without a Kind cannot pass for one
	// of the categories below, in either direction.
	DiagnosticUnclassified DiagnosticKind = iota
	// DiagnosticAdvisory reports a choice with no effect on the accepted set: the plan
	// accepts exactly what the schema does, and the note is about how it is stored,
	// dispatched or authored.
	DiagnosticAdvisory
	// DiagnosticCost reports that enforcing the schema exactly requires work at
	// runtime — match counting, trial validation, a residual sub-schema. The plan is
	// exact; [CompilationPlan.Capability] carries how expensive it is.
	DiagnosticCost
	// DiagnosticAssumed reports that acceptance rests on a declaration the compiler
	// could not verify, so the plan may accept more than the schema — but its own
	// machinery bounds the excess. Today: a discriminator whose branches are not
	// provably disjoint, where each branch still validates a mis-tagged instance.
	DiagnosticAssumed
	// DiagnosticUnenforced reports a construct the plan does not enforce and nothing
	// else closes: the plan accepts strictly more than the schema (§24.1). This is the
	// declared narrowing §24.2 permits, and the reason silence would not be.
	DiagnosticUnenforced
	// DiagnosticUnsupported reports a construct no plan of this design enforces, so
	// the plan is not one to generate from. It is unenforced too, but a caller that
	// refuses the capability level never reaches the excess.
	DiagnosticUnsupported
)

// Severity classifies a diagnostic.
type Severity uint8

const (
	// SeverityInfo notes a representation choice (e.g. over-approximation used).
	SeverityInfo Severity = iota
	// SeverityWarning notes a capability downgrade the caller may care about.
	SeverityWarning
	// SeverityError notes an unsupported construct.
	SeverityError
)
