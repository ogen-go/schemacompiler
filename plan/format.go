package plan

// Format is the `format` keyword as it reaches a representation (design §7): the name
// as written, plus a classification telling a backend whether the format names its own
// value domain or merely constrains the underlying primitive.
type Format struct {
	Name  string
	Class FormatClass
}

// FormatClass classifies a `format` by its effect on the representation.
type FormatClass uint8

const (
	// FormatNone means no `format` was present.
	FormatNone FormatClass = iota
	// FormatValidationOnly means the format restricts which instances are accepted, but
	// the value is still the primitive itself: the natural Go type stays unchanged.
	FormatValidationOnly
	// FormatRepresentational means the format names a value domain outside the JSON
	// lexical space, with a canonical in-memory form a backend is expected to decode
	// into (e.g. a timestamp, a UUID, an IP address).
	FormatRepresentational
	// FormatUnrecognized means the compiler assigns no meaning to the name; a backend
	// may still map it, the plan makes no claim either way.
	FormatUnrecognized
)

// formatClasses is the compiler's fixed registry of known `format` names, covering the
// JSON Schema format-annotation vocabulary plus the OpenAPI data-type formats.
var formatClasses = map[string]FormatClass{
	"date-time": FormatRepresentational,
	"date":      FormatRepresentational,
	"time":      FormatRepresentational,
	"duration":  FormatRepresentational,
	"uuid":      FormatRepresentational,
	"ipv4":      FormatRepresentational,
	"ipv6":      FormatRepresentational,
	"byte":      FormatRepresentational,
	"binary":    FormatRepresentational,
	"int32":     FormatRepresentational,
	"int64":     FormatRepresentational,
	"float":     FormatRepresentational,
	"double":    FormatRepresentational,
	"decimal":   FormatRepresentational,

	"email":                 FormatValidationOnly,
	"idn-email":             FormatValidationOnly,
	"hostname":              FormatValidationOnly,
	"idn-hostname":          FormatValidationOnly,
	"uri":                   FormatValidationOnly,
	"uri-reference":         FormatValidationOnly,
	"uri-template":          FormatValidationOnly,
	"iri":                   FormatValidationOnly,
	"iri-reference":         FormatValidationOnly,
	"json-pointer":          FormatValidationOnly,
	"relative-json-pointer": FormatValidationOnly,
	"regex":                 FormatValidationOnly,
	"password":              FormatValidationOnly,
}

// ClassifyFormat classifies a `format` name.
func ClassifyFormat(name string) FormatClass {
	if name == "" {
		return FormatNone
	}
	if c, ok := formatClasses[name]; ok {
		return c
	}
	return FormatUnrecognized
}

// NewFormat builds a [Format] from a `format` name, classifying it.
func NewFormat(name string) Format {
	return Format{Name: name, Class: ClassifyFormat(name)}
}

// Representational reports whether a backend should pick a dedicated Go type for f.
func (f Format) Representational() bool { return f.Class == FormatRepresentational }
