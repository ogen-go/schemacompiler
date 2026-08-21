package plan

import "strconv"

// Position locates a schema in the source document it was parsed from (issue #21).
//
// Line and Column are 1-based and zero when the parser retained no position, so a
// position degrades to its File (or to nothing) rather than reporting a bogus 0:0.
type Position struct {
	// File identifies the document: its retrieval URI, since an external `$ref` target
	// comes from a document other than the root one.
	File   string
	Line   int
	Column int
}

// IsZero reports whether no part of the position is known.
func (p Position) IsZero() bool {
	return p == Position{}
}

// String renders the position as "file:line:column", omitting the unknown parts.
func (p Position) String() string {
	if p.Line <= 0 {
		return p.File
	}
	at := strconv.Itoa(p.Line)
	if p.Column > 0 {
		at += ":" + strconv.Itoa(p.Column)
	}
	if p.File == "" {
		return at
	}
	return p.File + ":" + at
}
