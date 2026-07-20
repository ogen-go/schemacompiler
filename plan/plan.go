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
}

// Diagnostic explains why a stronger conversion was not possible (design §25).
type Diagnostic struct {
	// Pointer is the JSON Pointer to the offending schema location, when known.
	Pointer  string
	Severity Severity
	Message  string
}

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
