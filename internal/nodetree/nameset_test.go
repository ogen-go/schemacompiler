package nodetree

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNameSetHashesLikeItScans pins that [linearNameScan] is a performance knob and
// nothing else: both paths must answer the same index, including for a duplicate name,
// where the scan's first-match rule is the one the map has to reproduce.
func TestNameSetHashesLikeItScans(t *testing.T) {
	names := []string{"a", "b", "c", "a", "", "d/e", "~", "ünïcode"}

	scan := nameSet{names: names}
	hashed := newNameSet(names)
	require.NotNil(t, hashed.index, "the set must be past the scan threshold")

	for _, key := range append([]string{"z", "A", "aa", ""}, names...) {
		t.Run(strconv.Quote(key), func(t *testing.T) {
			wantIdx, wantOK := scan.lookup([]byte(key))
			gotIdx, gotOK := hashed.lookup([]byte(key))
			require.Equal(t, wantOK, gotOK)
			require.Equal(t, wantIdx, gotIdx)
		})
	}
}

// TestPresenceSpillsPastOneWord pins the bitset past the inline word, where an index is
// no longer its own bit position.
func TestPresenceSpillsPastOneWord(t *testing.T) {
	names := make([]string, 130)
	for i := range names {
		names[i] = strconv.Itoa(i)
	}
	set := newNameSet(names)

	p := set.newPresence()
	require.NotNil(t, p.words, "130 names do not fit one word")
	for _, i := range []int{0, 63, 64, 65, 129} {
		require.False(t, p.has(i))
		p.set(i)
		require.True(t, p.has(i))
	}
	require.False(t, p.has(1), "setting a bit must not disturb its neighbors")

	require.True(t, p.covers(set.maskOf([]string{"0", "64", "129"})))
	require.False(t, p.covers(set.maskOf([]string{"0", "100"})))
}

// BenchmarkNameSet measures the same crossover for [linearNameScan]: unlike a dispatch a
// lookup here usually misses, because an instance carries keys the schema never declared,
// and a miss is the full scan.
func BenchmarkNameSet(b *testing.B) {
	for _, n := range []int{2, 3, 4, 6, 8, 16, 64, 256} {
		set := newNameSet(declaredNames(n))
		scan := nameSet{names: set.names}
		hit := []byte("property-" + strconv.Itoa(n-1))
		miss := []byte("undeclared")

		for _, probe := range []struct {
			name string
			key  []byte
		}{{"hit", hit}, {"miss", miss}} {
			b.Run(strconv.Itoa(n)+"/"+probe.name+"/scan", func(b *testing.B) {
				for b.Loop() {
					scan.lookup(probe.key)
				}
			})
			b.Run(strconv.Itoa(n)+"/"+probe.name+"/table", func(b *testing.B) {
				hashed := nameSet{names: set.names, index: map[string]int{}}
				for i, nm := range set.names {
					hashed.index[nm] = i
				}
				b.ResetTimer()
				for b.Loop() {
					hashed.lookup(probe.key)
				}
			})
		}
	}
}

func declaredNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "property-" + strconv.Itoa(i)
	}
	return out
}
