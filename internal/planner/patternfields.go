package planner

import (
	"regexp"
	"strconv"
	"sync"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/plan"
)

// fieldConstraint is design §12.3's `constraintsFor(name)` for a name `properties`
// declares: the property's own schema intersected with every `patternProperties` schema
// whose pattern matches that name. Both apply, so a declared property that also matches a
// pattern must satisfy both.
//
// Folding the patterns in here is what lets a declared field own its property name
// outright: [plan.ObjectRepresentation] gives a property one storage slot, and the slot's
// plan has to carry the whole constraint on it, since nothing downstream re-derives which
// pattern rules a field name matches.
func (b *builder) fieldConstraint(name string, own ir.Expr, patterns []ir.PatternPropertyExpr, path string) ir.Expr {
	operands := []ir.Expr{own}
	for _, pp := range patterns {
		re, err := compilePattern(pp.Pattern)
		if err != nil {
			// Whether the pattern covers this name is undecidable here, and assuming it
			// does would reject instances the schema accepts — the one direction design
			// §24 forbids. Leaving it out only accepts more, which is sound, but nothing
			// else closes the gap (issue #84).
			b.dropped = true
			b.diag(path, plan.SeverityWarning, "patternProperties pattern "+strconv.Quote(pp.Pattern)+
				" does not compile as RE2; it is not intersected into the properties it may match")
			continue
		}
		if re.MatchString(name) {
			operands = append(operands, pp.Schema)
		}
	}
	if len(operands) == 1 {
		return own
	}
	// The intersection is composed here rather than by norm, so it has to be
	// re-normalized before a sub-builder sees it.
	return norm.Normalize(ir.All{Operands: operands}, renormalizeBudget)
}

// patternCache memoizes compilation across the properties of one object and across
// objects: the same handful of patterns is matched against every declared name.
var patternCache sync.Map // string -> *regexp.Regexp or error

// compilePattern compiles an ECMA-262 pattern as Go's RE2 reads it, unanchored, the way
// JSON Schema defines `patternProperties`. RE2 rejects what ECMA-262 has and it does not
// (lookaround, backreferences), which is the error the caller has to decide on.
func compilePattern(pattern string) (*regexp.Regexp, error) {
	if got, ok := patternCache.Load(pattern); ok {
		if re, ok := got.(*regexp.Regexp); ok {
			return re, nil
		}
		return nil, got.(error)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		patternCache.Store(pattern, err)
		return nil, err
	}
	patternCache.Store(pattern, re)
	return re, nil
}
