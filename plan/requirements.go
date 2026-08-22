package plan

// Location names a schema position a requirement applies to.
type Location struct {
	// Pointer is the JSON Pointer to the schema the requirement arises from, relative to
	// the document root, as [Diagnostic.Pointer] is.
	Pointer string
	// Position is the source location of that schema, when the parser retained one.
	Position Position
	// Detail names the construct, so a consumer can report the requirement without
	// re-deriving why it is there.
	Detail string
}

// Requirements is what a plan asks of the consumer that lowers it (design §25).
//
// The compiler chooses no Go types and emits no code, so it cannot say whether the
// generated program reproduces the schema's accepted set — that depends on how integers
// are sized, whether unknown properties are retained, which regex engine runs, none of
// which are its to decide (§25.1). What it can do is name the places where those
// decisions matter. A backend discharges them, and only then may it claim exactness.
//
// Every field over-reports rather than under-reports. A consumer that handles a
// requirement it did not strictly have to is correct; one that misses a real requirement
// is not, and the failure is silent (§24.3).
type Requirements struct {
	// RawEvaluation reports checks that inspect something decoding discards, so they
	// cannot be discharged against the decoded value alone (§24.3). A `minProperties`
	// over a struct is the shape of it: the count includes names the struct has no field
	// for and drops.
	RawEvaluation []Location

	// UnboundedNumeric reports numeric slots the schema does not bound, so a fixed-width
	// Go type narrows them and must declare the narrowing (§24.2). A slot the schema
	// bounds into range is not listed: there the narrowing is discharged statically.
	UnboundedNumeric []Location

	// JSONEquality reports checks defined by equality of JSON values rather than of the
	// decoded Go value. `uniqueItems` is the one that bites: two JSON-distinct items that
	// decode to the same Go value make it reject an array the schema accepts.
	JSONEquality []Location

	// ECMARegex reports patterns that do not mean the same thing under RE2, so Go's
	// stdlib `regexp` is not a correct engine for them (§11.10).
	ECMARegex []Location

	// EvaluationTracking reports checks needing evaluated-location annotations,
	// `unevaluatedProperties` and `unevaluatedItems`.
	EvaluationTracking []Location
}

// Empty reports whether the plan asks nothing of its consumer.
func (r Requirements) Empty() bool {
	return len(r.RawEvaluation) == 0 &&
		len(r.UnboundedNumeric) == 0 &&
		len(r.JSONEquality) == 0 &&
		len(r.ECMARegex) == 0 &&
		len(r.EvaluationTracking) == 0
}
