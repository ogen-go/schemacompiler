package planterp

import "strings"

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerAppend extends a JSON Pointer (RFC 6901) with one reference token.
func pointerAppend(path, token string) string {
	return path + "/" + pointerEscaper.Replace(token)
}

// instanceLocation names where in the instance an [InternalError] was raised. One plan
// node is reached once per matching sub-value, so a malformed slot is only diagnosable
// alongside the location that found it.
func instanceLocation(f frame) string {
	if f.path == "" {
		return "the instance root"
	}
	return f.path
}
