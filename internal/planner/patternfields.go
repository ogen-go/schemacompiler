package planner

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// constraintsFor is design §12.3's `constraintsFor(name)` for one schema object: the
// property's own schema plus every `patternProperties` schema whose pattern matches the
// name, falling back to that object's `additionalProperties` when neither matches. The
// fallback is what scopes `additionalProperties` to the object that declared it, so a name
// an applicator declared is still additional here (issue #94).
//
// Folding the patterns in here is what lets a declared field own its property name
// outright: [plan.ObjectRepresentation] gives a property one storage slot, and the slot's
// plan has to carry the whole constraint on it, since nothing downstream re-derives which
// pattern rules a field name matches.
//
// Matching goes through [ecmaregex] rather than Go's RE2, so the answer is the one the
// schema's own language gives and the one ogen's runtime will give (issue #111).
//
// The operands are returned unconjoined so the caller can intersect them across every
// conjoined schema object at once.
func (b *builder) constraintsFor(name string, os ir.ObjectShape, path string) []ir.Expr {
	var (
		operands  []ir.Expr
		undecided bool
	)
	for _, p := range os.Properties {
		if p.Name == name {
			operands = append(operands, p.Schema)
		}
	}
	for _, pp := range os.PatternProperties {
		b.requireECMARegex(pp.Pattern, path, "patternProperties")
		switch res, err := ecmaregex.MatchString(pp.Pattern, name); res {
		case ecmaregex.Match:
			operands = append(operands, pp.Schema)
		case ecmaregex.Unknown:
			// Whether the pattern covers this name is undecidable here, and assuming it
			// does would reject instances the schema accepts — the one direction design
			// §24 forbids. Leaving it out only accepts more, which is sound, but nothing
			// else closes the gap (issue #84). It also leaves it undecidable whether this
			// object's `additionalProperties` covers the name, so that is dropped too.
			b.dropped = true
			undecided = true
			b.diag(path, plan.SeverityWarning, "patternProperties pattern "+strconv.Quote(pp.Pattern)+
				" could not be matched as ECMA-262 ("+err.Error()+"); "+
				"it is not intersected into the properties it may match")
		}
	}
	if len(operands) > 0 || undecided {
		return operands
	}
	if os.AdditionalProperties != nil {
		return []ir.Expr{os.AdditionalProperties}
	}
	return nil
}
