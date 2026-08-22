package ecmaregex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
)

// TestMatchString pins the places ECMA-262 and RE2 disagree, since agreeing with RE2 is
// exactly the defect this package exists to avoid (issue #111).
func TestMatchString(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    ecmaregex.Result
	}{
		{
			name:    "unanchored search",
			pattern: "b",
			input:   "abc",
			want:    ecmaregex.Match,
		},
		{
			name:    "no match",
			pattern: "^x",
			input:   "abc",
			want:    ecmaregex.NoMatch,
		},
		{
			// RE2 reads \s as [\t\n\f\r ] and answers NoMatch here.
			name:    "whitespace class covers non-ASCII",
			pattern: `^\s$`,
			input:   " ",
			want:    ecmaregex.Match,
		},
		{
			// RE2 cannot compile lookbehind at all.
			name:    "lookbehind",
			pattern: `(?<=a)b`,
			input:   "ab",
			want:    ecmaregex.Match,
		},
		{
			name:    "backreference",
			pattern: `^(a+)\1$`,
			input:   "aaaa",
			want:    ecmaregex.Match,
		},
		{
			name:    "control escape",
			pattern: `^\cC$`,
			input:   "\x03",
			want:    ecmaregex.Match,
		},
		{
			name:    "short unicode property",
			pattern: `\p{L}`,
			input:   "a",
			want:    ecmaregex.Match,
		},
		{
			// The one direction RE2 is the more permissive engine: it accepts the long
			// property name and ECMA-262 does not.
			name:    "long unicode property is undecidable",
			pattern: `\p{Letter}`,
			input:   "a",
			want:    ecmaregex.Unknown,
		},
		{
			name:    "malformed pattern is undecidable",
			pattern: "(a",
			input:   "a",
			want:    ecmaregex.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ecmaregex.MatchString(tt.pattern, tt.input)
			require.Equal(t, tt.want, got)
			if tt.want == ecmaregex.Unknown {
				require.Error(t, err, "Unknown must carry its reason")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
