package planterp

import (
	"regexp"
	"strconv"
	"sync"
)

// matchPattern applies an ECMA-262 pattern as an unanchored search, the way JSON Schema
// defines `pattern` and `patternProperties`. Go's RE2 rejects the constructs ECMA-262
// has and RE2 does not (lookaround, backreferences); such a pattern is recorded as
// unenforceable and matches everything, since refusing to match would under-approximate.
func (in *interp) matchPattern(pattern, s string) bool {
	re, err := compilePattern(pattern)
	if err != nil {
		in.approximate("pattern " + strconv.Quote(pattern) + " does not compile as RE2")
		return true
	}
	return re.MatchString(s)
}

// patternCache memoizes compilation across instances: the suite runs thousands of them
// against the same handful of patterns.
var patternCache sync.Map // string -> *regexp.Regexp or error

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
