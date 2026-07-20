package planner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
)

func lit(value any, raw string) ir.Literal {
	return ir.Literal{Value: value, Raw: []byte(raw)}
}

func TestValueSet(t *testing.T) {
	t.Run("composite values do not panic and dedup", func(t *testing.T) {
		s := newValueSet(4)
		require.True(t, s.add(lit([]any{"a", "b"}, `["a","b"]`)))
		require.False(t, s.add(lit([]any{"a", "b"}, `["a","b"]`)), "duplicate array")
		require.True(t, s.add(lit(map[string]any{"k": 1.0}, `{"k":1}`)))
		require.False(t, s.add(lit(map[string]any{"k": 1.0}, `{"k":1}`)), "duplicate object")
	})

	t.Run("semantic equality across spellings", func(t *testing.T) {
		s := newValueSet(4)
		require.True(t, s.add(lit(1.0, `1.0`)))
		require.False(t, s.add(lit(1.0, `1`)), "1.0 and 1 are the same JSON number")
		require.True(t, s.add(lit(nil, `{"a":1,"b":2}`)))
		require.False(t, s.add(lit(nil, `{"b":2,"a":1}`)), "object key order is irrelevant")
	})

	t.Run("high-precision integers stay distinct", func(t *testing.T) {
		s := newValueSet(2)
		require.True(t, s.add(lit(9.007199254740992e15, `9007199254740992`)))
		require.True(t, s.add(lit(9.007199254740992e15, `9007199254740993`)),
			"distinct integers past 2^53 must not be deduped despite equal float64")
	})

	t.Run("falls back to DeepEqual without raw bytes", func(t *testing.T) {
		s := newValueSet(2)
		require.True(t, s.add(ir.Literal{Value: "x"}))
		require.False(t, s.add(ir.Literal{Value: "x"}))
		require.True(t, s.add(ir.Literal{Value: "y"}))
	})
}
