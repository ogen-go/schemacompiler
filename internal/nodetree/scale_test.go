package nodetree_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// names builds n property names, past the 64 a single bitmask can hold.
func names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + strconv.Itoa(i)
	}
	return out
}

func quoteJoin(names []string) string {
	return `"` + strings.Join(names, `","`) + `"`
}

func objectWith(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `"` + n + `":1`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestRequiredBeyondSixtyFourNames pins that the bitmask is an optimization, not a limit:
// a schema naming more than 64 required properties still compiles and still enforces them.
func TestRequiredBeyondSixtyFourNames(t *testing.T) {
	want := names("p", 70)
	v := compile(t, `{"type":"object","required":[`+quoteJoin(want)+`]}`)

	require.True(t, v.IsValid([]byte(objectWith(want))))
	require.False(t, v.IsValid([]byte(objectWith(want[:69]))), "the 70th name is still required")
	require.False(t, v.IsValid([]byte(objectWith(want[1:]))), "the first name is still required")
}

// TestDependentRequiredBeyondSixtyFourNames pins the same for dependentRequired, whose
// name list is the union over every entry and so overflows 64 sooner than the author's
// own reading of the schema suggests.
func TestDependentRequiredBeyondSixtyFourNames(t *testing.T) {
	req := names("r", 70)
	v := compile(t, `{"type":"object","dependentRequired":{"a":[`+quoteJoin(req)+`]}}`)

	require.True(t, v.IsValid([]byte(objectWith(req))), "the trigger is absent, so nothing is required")
	require.True(t, v.IsValid([]byte(objectWith(append([]string{"a"}, req...)))),
		"every dependent name is present")
	require.False(t, v.IsValid([]byte(objectWith(append([]string{"a"}, req[:69]...)))),
		"the 70th dependent name is missing")
}

// TestObjectStructureBeyondSixtyFourProperties pins that a wide `properties` still
// compiles and still checks the property past the 64th.
func TestObjectStructureBeyondSixtyFourProperties(t *testing.T) {
	decl := names("p", 70)
	parts := make([]string, len(decl))
	for i, n := range decl {
		parts[i] = `"` + n + `":{"type":"integer"}`
	}
	v := compile(t, `{"type":"object","properties":{`+strings.Join(parts, ",")+`}}`)

	require.True(t, v.IsValid([]byte(objectWith(decl))))
	require.False(t, v.IsValid([]byte(`{"p69":"not an integer"}`)), "the 70th property is still checked")
}
