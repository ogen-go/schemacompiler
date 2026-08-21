package planterp

import "strings"

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerAppend extends a JSON Pointer (RFC 6901) with one reference token.
func pointerAppend(path, token string) string {
	return path + "/" + pointerEscaper.Replace(token)
}
