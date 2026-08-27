package opt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/opt"
)

func TestMarshal(t *testing.T) {
	for _, tt := range []struct {
		name string
		v    any
		want string
	}{
		{"set opt", opt.Some("x"), `"x"`},
		{"unset opt", opt.Opt[string]{}, `null`},
		{"non-null", opt.NonNull(3), `3`},
		{"null", opt.Nullable[int]{}, `null`},
		{"present", opt.Present("x"), `"x"`},
		{"explicit null", opt.Null[string](), `null`},
		{"absent", opt.OptNullable[string]{}, `null`},
		{"nested", opt.Some([]int{1, 2}), `[1,2]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.v)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestUnmarshalRoundTrips(t *testing.T) {
	var o opt.Opt[string]
	require.NoError(t, json.Unmarshal([]byte(`"x"`), &o))
	v, ok := o.Get()
	require.True(t, ok)
	require.Equal(t, "x", v)

	// Opt has no null state, so a null leaves it unset rather than inventing one.
	require.NoError(t, json.Unmarshal([]byte(`null`), &o))
	require.False(t, o.IsSet())

	var n opt.Nullable[int]
	require.NoError(t, json.Unmarshal([]byte(`7`), &n))
	require.False(t, n.IsNull())
	require.NoError(t, json.Unmarshal([]byte(`null`), &n))
	require.True(t, n.IsNull())

	// A written null is null, not absent: the key was there.
	var on opt.OptNullable[string]
	require.NoError(t, json.Unmarshal([]byte(`null`), &on))
	require.True(t, on.IsNull())
	require.False(t, on.IsAbsent())

	require.NoError(t, json.Unmarshal([]byte(`"y"`), &on))
	require.True(t, on.IsSet())
}

func TestUnmarshalReportsTheInnerError(t *testing.T) {
	var o opt.Opt[int]
	require.Error(t, json.Unmarshal([]byte(`"nope"`), &o))
	require.False(t, o.IsSet(), "a failed decode must not leave a value behind")
}

// TestMarshalIsNotTheZeroStruct is the whole reason these methods exist: without them
// encoding/json sees no exported fields and writes `{}` for every presence-wrapped value.
func TestMarshalIsNotTheZeroStruct(t *testing.T) {
	got, err := json.Marshal(struct {
		A opt.Opt[string]
		B opt.Nullable[int]
	}{A: opt.Some("x"), B: opt.NonNull(1)})
	require.NoError(t, err)
	require.JSONEq(t, `{"A":"x","B":1}`, string(got))
}
