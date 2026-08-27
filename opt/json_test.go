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

	// Opt is what a schema admitting no null lowers to, so a null here is a value that
	// schema rejects. Taking it as absence would accept it and lose the difference.
	require.ErrorContains(t, json.Unmarshal([]byte(`null`), &o), "not an admitted value")

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

// TestOmitzeroOmitsAbsent is what the generated struct tags rely on. `omitempty` does
// nothing at all for a struct type, so without IsZero — and without `omitzero` naming it —
// an absent value would be written as null under its own key.
func TestOmitzeroOmitsAbsent(t *testing.T) {
	type doc struct {
		A opt.Opt[string]         `json:"a,omitzero"`
		B opt.OptNullable[string] `json:"b,omitzero"`
		C opt.Nullable[string]    `json:"c"`
		D opt.Opt[string]         `json:"d,omitempty"`
	}

	got, err := json.Marshal(doc{})
	require.NoError(t, err)
	require.JSONEq(t, `{"c":null,"d":null}`, string(got),
		"omitzero drops a and b; omitempty does nothing for d, which is why it is not used")

	got, err = json.Marshal(doc{A: opt.Some("x"), B: opt.Null[string]()})
	require.NoError(t, err)
	require.JSONEq(t, `{"a":"x","b":null,"c":null,"d":null}`, string(got),
		"an explicit null is written; only absence is omitted")
}

// TestNullableHasNoIsZero pins the asymmetry: Nullable's zero value is null, and null is a
// value the schema admits, so omitzero must never drop it.
func TestNullableHasNoIsZero(t *testing.T) {
	var n any = opt.Nullable[string]{}
	_, ok := n.(interface{ IsZero() bool })
	require.False(t, ok, "Nullable must not have IsZero, or omitzero would drop an admitted null")

	var o any = opt.Opt[string]{}
	_, ok = o.(interface{ IsZero() bool })
	require.True(t, ok)
}

// TestUnsetClearsTheValue keeps two absent values equal. They are uncomparable by ==, so a
// retained value would show up through reflect.DeepEqual and in every require.Equal on a
// struct containing one — and would keep a large value alive.
func TestUnsetClearsTheValue(t *testing.T) {
	used := opt.Some([]byte("a large value"))
	used.Unset()
	require.Equal(t, opt.Opt[[]byte]{}, used)

	nn := opt.Present("x")
	nn.Unset()
	require.Equal(t, opt.OptNullable[string]{}, nn)

	nn.Set("y")
	nn.SetNull()
	require.Equal(t, opt.Null[string](), nn)

	n := opt.NonNull("x")
	n.SetNull()
	require.Equal(t, opt.Nullable[string]{}, n)
}
