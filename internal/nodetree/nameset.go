package nodetree

import "github.com/go-faster/jx"

// linearNameScan is the size up to which lookup scans instead of hashing. Three names is
// where BenchmarkNameSet has the two even; below it the scan is ahead and costs no map.
const linearNameScan = 3

// nameSet maps a property name to its position in the list the plan declared. Duplicates
// resolve to the first position, in both the scanned and the hashed path, so an index is
// the same answer either way.
type nameSet struct {
	names []string
	index map[string]int
}

func newNameSet(names []string) nameSet {
	s := nameSet{names: names}
	if len(names) <= linearNameScan {
		return s
	}
	s.index = make(map[string]int, len(names))
	for i, n := range names {
		if _, dup := s.index[n]; !dup {
			s.index[n] = i
		}
	}
	return s
}

func (s nameSet) lookup(key []byte) (int, bool) {
	if s.index == nil {
		for i, n := range s.names {
			if n == string(key) {
				return i, true
			}
		}
		return 0, false
	}
	i, ok := s.index[string(key)]
	return i, ok
}

func (s nameSet) indexOf(name string) (int, bool) {
	return s.lookup([]byte(name))
}

// presence is a bit per [nameSet] position. Up to 64 names — every schema anyone writes by
// hand — live in the inline word and allocate nothing; wider sets spill into words.
type presence struct {
	word  uint64
	words []uint64
}

func (s nameSet) newPresence() presence {
	if len(s.names) <= 64 {
		return presence{}
	}
	return presence{words: make([]uint64, (len(s.names)+63)/64)}
}

func (p *presence) set(i int) {
	if p.words == nil {
		p.word |= 1 << uint(i)
		return
	}
	p.words[i/64] |= 1 << uint(i%64)
}

func (p presence) has(i int) bool {
	if p.words == nil {
		return p.word&(1<<uint(i)) != 0
	}
	return p.words[i/64]&(1<<uint(i%64)) != 0
}

func (p presence) covers(want presence) bool {
	if p.words == nil {
		return p.word&want.word == want.word
	}
	for i, w := range want.words {
		if p.words[i]&w != w {
			return false
		}
	}
	return true
}

// maskOf marks every name that belongs to s, for comparison against what an instance
// turned out to carry.
func (s nameSet) maskOf(names []string) presence {
	p := s.newPresence()
	for _, n := range names {
		if i, ok := s.indexOf(n); ok {
			p.set(i)
		}
	}
	return p
}

// presenceOf walks the instance's keys once and records which of s it carries. The second
// result is false when raw is not an object, which a guard beside the caller handles.
func (s nameSet) presenceOf(raw []byte) (presence, bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	p := s.newPresence()
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if i, ok := s.lookup(key); ok {
			p.set(i)
		}
		return d.Skip()
	}); err != nil {
		return presence{}, false
	}
	return p, true
}
