package planterp

import (
	"strconv"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
)

// matchTimeout bounds one match. regexp2 backtracks, so a pathological pattern can run
// for effectively unbounded time where RE2 could not; the bound turns that into a
// [patternUnknown] instead of a hung process.
const matchTimeout = 250 * time.Millisecond

// patternResult is a match outcome that can also be undecided. The undecided case is not
// foldable into either boolean: `pattern` wants it to accept, while a `patternProperties`
// rule wants it to claim the name without constraining it, and those are opposite
// booleans.
type patternResult int

const (
	patternNoMatch patternResult = iota
	patternMatch
	patternUnknown
)

// matchPattern applies an ECMA-262 pattern as an unanchored search, the way JSON Schema
// defines `pattern` and `patternProperties`. A pattern that does not compile, or a match
// that does not finish within [matchTimeout], is recorded as unenforceable and reported
// as [patternUnknown]: design §24 forbids under-approximation, so a constraint the
// interpreter cannot decide must never be the reason an instance is rejected.
func (in *interp) matchPattern(pattern, s string) patternResult {
	re, err := compilePattern(pattern)
	if err != nil {
		in.approximate("pattern " + strconv.Quote(pattern) + " does not compile as ECMA-262: " + err.Error())
		return patternUnknown
	}
	ok, err := re.MatchString(s)
	if err != nil {
		in.approximate("pattern " + strconv.Quote(pattern) + " did not finish matching: " + err.Error())
		return patternUnknown
	}
	if ok {
		return patternMatch
	}
	return patternNoMatch
}

// patternCache memoizes compilation across instances: the suite runs thousands of them
// against the same handful of patterns.
var patternCache sync.Map // string -> *regexp2.Regexp or error

func compilePattern(pattern string) (*regexp2.Regexp, error) {
	if got, ok := patternCache.Load(pattern); ok {
		if re, ok := got.(*regexp2.Regexp); ok {
			return re, nil
		}
		return nil, got.(error)
	}
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		patternCache.Store(pattern, err)
		return nil, err
	}
	re.MatchTimeout = matchTimeout
	patternCache.Store(pattern, re)
	return re, nil
}
