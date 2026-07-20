package plan

// JSONKind is a single JSON syntactic kind.
type JSONKind uint8

// The JSON syntactic kinds.
const (
	KindNull JSONKind = iota
	KindBoolean
	KindNumber
	KindString
	KindArray
	KindObject
)

// KindSet is an abstract set of possible JSON kinds carried by every expression
// and plan node (design §6). integer is modeled as a numeric-domain refinement of
// KindNumber, not a distinct kind.
type KindSet uint8

// The single-kind bits of a [KindSet].
const (
	SetNull KindSet = 1 << iota
	SetBoolean
	SetNumber
	SetString
	SetArray
	SetObject
)

// SetAny is every JSON kind.
const SetAny = SetNull | SetBoolean | SetNumber | SetString | SetArray | SetObject

// Has reports whether k is a member of the set.
func (s KindSet) Has(k JSONKind) bool { return s&(1<<k) != 0 }

// NumericDomain refines KindNumber into the integer / non-integer distinction (design §6).
type NumericDomain uint8

// The numeric domains of a [NumericDomain].
const (
	AnyNumber NumericDomain = iota
	IntegerOnly
	NonIntegerOnly
)
