package planner

import "strings"

var pointerSegmentEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerAppend appends segment to a diagnostic path, escaping it the way the frontend
// escapes JSON Pointer segments (RFC 6901) so a property named "a/b" does not read as two
// segments.
func pointerAppend(path, segment string) string {
	return path + "/" + pointerSegmentEscaper.Replace(segment)
}
