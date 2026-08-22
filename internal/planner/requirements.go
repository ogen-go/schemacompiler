package planner

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// requireECMARegex records a pattern that RE2 does not read the same way, so a consumer
// using Go's stdlib `regexp` would enforce something other than what the schema wrote
// (design §11.10, issue #111).
//
// Two things make a pattern differ, and the check is deliberately conservative in both
// directions — over-reporting costs a backend an engine it did not strictly need, while
// under-reporting is silent and wrong:
//
//   - RE2 cannot compile it at all (lookaround, backreferences), or ECMA-262 cannot
//     (`\p{Letter}`). Either way the two engines do not agree.
//   - Both compile it, but a class means different things. RE2 reads `\s`, `\d` and `\w`
//     as ASCII-only and ECMA-262 does not, and the Unicode property syntaxes differ.
//     Whether a *particular* pattern's meaning actually diverges is undecidable, so the
//     presence of such an escape is reported as-is.
func (b *builder) requireECMARegex(pattern, path, keyword string) {
	if !re2Agrees(pattern) {
		b.require(&b.reqs.ECMARegex, path, keyword+" "+strconv.Quote(pattern))
	}
}

// classEscapes are the escapes RE2 and ECMA-262 read differently.
var classEscapes = []string{`\s`, `\S`, `\d`, `\D`, `\w`, `\W`, `\p{`, `\P{`}

func re2Agrees(pattern string) bool {
	if _, err := regexp.Compile(pattern); err != nil {
		return false
	}
	for _, esc := range classEscapes {
		if strings.Contains(pattern, esc) {
			return false
		}
	}
	return true
}

// requireNumericBound records a numeric slot no bound narrows into a fixed-width Go type,
// so lowering it to one is a narrowing that must be declared (design §24.2).
//
// A slot the schema bounds is not listed: `{"type":"integer","maximum":1000}` is provably
// adequate as an int64, and reporting it would train a consumer to ignore the field. The
// bound has to come from the slot's own validation — a `minimum`/`maximum` pair on the
// same node — since nothing else in the plan constrains the magnitude.
func (b *builder) requireNumericBound(val plan.ValidationPlan, path string) {
	var low, high bool
	for _, gp := range val.Predicates {
		switch gp.Expression.(type) {
		case plan.MinimumPredicate:
			low = true
		case plan.MaximumPredicate:
			high = true
		}
	}
	if low && high {
		return
	}
	b.require(&b.reqs.UnboundedNumeric, path, "number is not bounded into a fixed-width type")
}
