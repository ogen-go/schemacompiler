package dump

import (
	"fmt"
	"io"
)

// indentWidth is the number of spaces added per nesting level.
const indentWidth = 2

// tw is a small indenting line writer shared by the Expr and Plan formatters.
type tw struct {
	w     io.Writer
	depth int
}

// line writes one indented, newline-terminated line.
func (t *tw) line(format string, args ...any) {
	_, _ = fmt.Fprint(t.w, indent(t.depth)+fmt.Sprintf(format, args...)+"\n")
}

// enter runs fn with the indentation depth increased by one level.
func (t *tw) enter(fn func()) {
	t.depth++
	fn()
	t.depth--
}

func indent(depth int) string {
	if depth == 0 {
		return ""
	}
	b := make([]byte, depth*indentWidth)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
