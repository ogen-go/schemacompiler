// Package ecmaregex matches JSON Schema patterns with the language JSON Schema writes
// them in.
//
// `pattern` and `patternProperties` are ECMA-262 regular expressions. Go's stdlib
// `regexp` is RE2, a different language: it rejects lookaround, backreferences and
// `\cX`, and it reads `\s`, `\d` and `\w` as ASCII-only where ECMA-262 does not. Both
// divergences reach the compiler's output — the first as a dropped constraint, the
// second silently as a wrong one — so the compiler and the differential oracle both
// match through [github.com/dlclark/regexp2] in ECMAScript mode, which is also the
// engine ogen's generated code uses.
package ecmaregex

import (
	"sync"
	"time"

	"github.com/dlclark/regexp2"
)

// matchTimeout bounds one match. regexp2 backtracks, so a pathological pattern can run
// for effectively unbounded time where RE2 could not; the bound turns that into
// [Unknown] instead of a hung process.
const matchTimeout = 250 * time.Millisecond

// Result is a match outcome that can also be undecided. The undecided case is not
// foldable into either boolean: a `pattern` assertion wants it to accept, while a
// `patternProperties` rule wants it to claim the name without constraining it, and those
// are opposite booleans. Only the caller knows which way widens its own accepted set.
type Result int

const (
	NoMatch Result = iota
	Match
	Unknown
)

// MatchString applies pattern to s as an unanchored search, the way JSON Schema defines
// `pattern` and `patternProperties`.
//
// A pattern that does not compile, or a match that does not finish within
// [matchTimeout], is [Unknown] with the reason as the error. Neither is ever reported as
// a match or a non-match: design §24 forbids under-approximation, so a pattern that
// cannot be decided must not become the reason an instance is rejected. Match and NoMatch
// always come with a nil error, and Unknown never does.
func MatchString(pattern, s string) (Result, error) {
	re, err := compile(pattern)
	if err != nil {
		return Unknown, err
	}
	ok, err := re.MatchString(s)
	if err != nil {
		return Unknown, err
	}
	if ok {
		return Match, nil
	}
	return NoMatch, nil
}

// cache memoizes compilation. The oracle matches thousands of instances against the same
// handful of patterns, and the planner matches one pattern against every declared
// property name of an object.
var cache sync.Map // string -> *regexp2.Regexp or error

func compile(pattern string) (*regexp2.Regexp, error) {
	if got, ok := cache.Load(pattern); ok {
		if re, ok := got.(*regexp2.Regexp); ok {
			return re, nil
		}
		return nil, got.(error)
	}
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		cache.Store(pattern, err)
		return nil, err
	}
	re.MatchTimeout = matchTimeout
	cache.Store(pattern, re)
	return re, nil
}
