package planterp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func patternPlan(regex string) plan.CompilationPlan {
	return plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Validation:     guarded(plan.SetString, plan.PatternPredicate{Regex: regex}),
	}
}

// TestPatternIsECMA262 pins the constructs and class semantics that separate ECMA-262 —
// the dialect JSON Schema's `pattern` is written in, and the one ogen's runtime engine
// implements — from Go's RE2, which the interpreter used to approximate them all away with.
func TestPatternIsECMA262(t *testing.T) {
	tests := []struct {
		name   string
		regex  string
		value  string
		accept bool
	}{
		{name: "lookahead holds", regex: "^a(?=b)", value: "ab", accept: true},
		{name: "lookahead rejects", regex: "^a(?=b)", value: "ac"},
		{name: "lookbehind holds", regex: "(?<=a)b$", value: "ab", accept: true},
		{name: "lookbehind rejects", regex: "(?<=a)b$", value: "cb"},
		{name: "backreference holds", regex: `^(\w)\1$`, value: "aa", accept: true},
		{name: "backreference rejects", regex: `^(\w)\1$`, value: "ab"},
		{name: "control escape holds", regex: `^\cC$`, value: "\u0003", accept: true},
		{name: "control escape rejects the literal spelling", regex: `^\cC$`, value: `\cC`},

		{name: "d is ascii digits", regex: `^\d$`, value: "0", accept: true},
		{name: "d excludes non-ascii digits", regex: `^\d$`, value: "߀"},
		{name: "w is ascii letters", regex: `^\w$`, value: "a", accept: true},
		{name: "w excludes latin-1 letters", regex: `^\w$`, value: "é"},
		{name: "s covers the non-breaking space", regex: `^\s$`, value: "\u00a0", accept: true},
		{name: "s covers the byte order mark", regex: `^\s$`, value: "\ufeff", accept: true},
		{name: "s covers the paragraph separator", regex: `^\s$`, value: "\u2029", accept: true},
		{name: "s excludes a non-whitespace control", regex: `^\s$`, value: "\u0001"},

		// JSON Schema defines `pattern` as a search, not a full match.
		{name: "the match is unanchored", regex: "a", value: "xax", accept: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verdict, err := planterp.Interpret(patternPlan(tt.regex), tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.accept, verdict.Accepted, "reason: %s", verdict.Reason)
			require.Empty(t, verdict.Approximated)
		})
	}
}

// TestUncompilablePatternAccepts keeps a pattern the engine cannot read from becoming the
// reason an instance is rejected (design §24), and keeps the acceptance from passing for
// evidence that the plan is exact.
func TestUncompilablePatternAccepts(t *testing.T) {
	verdict, err := planterp.Interpret(patternPlan("("), "anything")
	require.NoError(t, err)
	require.True(t, verdict.Accepted)
	require.NotEmpty(t, verdict.Approximated)
}

// TestUncompilablePatternRuleClaimsTheProperty is the same soundness rule on the
// `patternProperties` side, where both booleans can reject: running the rule's plan on a
// name it may not cover, or falling through to an `additionalProperties: false`.
func TestUncompilablePatternRuleClaimsTheProperty(t *testing.T) {
	p := leaf(plan.ObjectRepresentation{
		Additional:   &[]plan.CompilationPlan{leaf(plan.NeverRepresentation{})}[0],
		PatternRules: []plan.PatternFieldRepresentation{{Pattern: "(", Plan: leaf(plan.NeverRepresentation{})}},
	})

	verdict, err := planterp.Interpret(p, decode(t, `{"x": 1}`))
	require.NoError(t, err)
	require.True(t, verdict.Accepted, "reason: %s", verdict.Reason)
	require.NotEmpty(t, verdict.Approximated)
}

// TestCatastrophicPatternTerminates is the price of an ECMA-262 engine: regexp2 backtracks
// where RE2 was linear, so a pattern like this one runs for exponential time on a
// non-matching input. The bounded match resolves the way every unenforceable constraint
// does — accept, and say so — rather than hanging the caller.
func TestCatastrophicPatternTerminates(t *testing.T) {
	value := strings.Repeat("a", 64) + "!"

	start := time.Now()
	verdict, err := planterp.Interpret(patternPlan("^(a+)+$"), value)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.True(t, verdict.Accepted)
	require.NotEmpty(t, verdict.Approximated)
	require.Less(t, elapsed, 10*time.Second)
}
