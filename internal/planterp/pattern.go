package planterp

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
)

// patternResult aliases [ecmaregex.Result] so the instance-directed code below reads in
// its own vocabulary.
type patternResult = ecmaregex.Result

const (
	patternNoMatch = ecmaregex.NoMatch
	patternMatch   = ecmaregex.Match
	patternUnknown = ecmaregex.Unknown
)

// matchPattern applies an ECMA-262 pattern as an unanchored search. A pattern the
// engine cannot decide is recorded as unenforceable, so an acceptance that depended on
// it does not read as evidence that the plan is exact.
func (in *interp) matchPattern(pattern, s string) patternResult {
	res, err := ecmaregex.MatchString(pattern, s)
	if res == patternUnknown {
		in.approximate("pattern " + strconv.Quote(pattern) + " could not be matched as ECMA-262: " + err.Error())
	}
	return res
}
