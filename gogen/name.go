package gogen

import (
	"go/token"
	"strings"
	"unicode"

	"github.com/go-faster/errors"
)

// NameExtension is the `x-*` keyword that names a schema's Go type outright, overriding
// the derived name. The compiler assigns no meaning to any extension
// ([plan.Metadata.Extensions]), so this is the backend's own contract with the author.
const NameExtension = "x-go-name"

// containerKeywords are the pointer segments whose *child* segment is the name: the
// keyword is a container, and skipping it is what makes `/components/schemas/Pet` yield
// `Pet` rather than `ComponentsSchemasPet`.
//
// `patternProperties` is here despite its children being regexes rather than names, so a
// rule reaches only the word characters of the pattern: `^a.*$` names `A`. Two patterns
// whose word characters agree collide, which is a hard error, and that is the only guard
// there is — a regex is not a name and an author with more than one should set
// [NameExtension].
var containerKeywords = map[string]bool{
	"components":        true,
	"schemas":           true,
	"definitions":       true,
	"$defs":             true,
	"properties":        true,
	"patternProperties": true,
	"dependentSchemas":  true,
}

// keywordAliases spell four positions the way a Go author would name them. Every other
// keyword contributes its own name, camel-cased like any segment: `/not` is `Not`, and
// `/propertyNames` is `PropertyNames`.
var keywordAliases = map[string]string{
	"items":       "Item",
	"prefixItems": "Item",
	"oneOf":       "Variant",
	"anyOf":       "Variant",
	"allOf":       "Member",
}

// TypeName derives the Go type name for the schema at pointer, a JSON Pointer in the space
// [plan.SchemaID] and [plan.Diagnostic.Pointer] use.
//
// The rule is mechanical and has no heuristics in it (docs/backend.md §1): container
// keywords drop out, four positions get a readable alias, and every remaining segment is
// split on `[-_. ]` and camel-cased. There is no initialism table, no pluralization, and
// `title` never participates — a name the author did not write is one the author cannot
// predict, and one that changes under them when a title is edited.
//
// A segment that does not survive into a Go identifier is an error rather than something
// to sanitize, so a name is always either the author's or mechanically theirs. The fix is
// always the same: set [NameExtension].
func TypeName(pointer string) (string, error) {
	var b strings.Builder
	for raw := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		if raw == "" {
			continue
		}
		seg := unescapePointer(raw)
		if containerKeywords[seg] {
			continue
		}
		if alias, ok := keywordAliases[seg]; ok {
			b.WriteString(alias)
			continue
		}
		part := camel(seg)
		if part == "" {
			return "", errors.Errorf("pointer %q: segment %q contributes no name; set %s", pointer, seg, NameExtension)
		}
		b.WriteString(part)
	}

	name := b.String()
	if name == "" {
		return "", errors.Errorf("pointer %q names no schema position; set %s", pointer, NameExtension)
	}
	if err := checkIdentifier(name); err != nil {
		return "", errors.Wrapf(err, "pointer %q derives %q", pointer, name)
	}
	return name, nil
}

// camel splits a segment on everything that cannot spell a Go identifier and upper-cases
// each word, so `@odata.location` is `OdataLocation` and `$ref` is `Ref`.
//
// Dropping a rune that has no place in an identifier is not the same as guessing a name:
// nothing is invented, and two names that drop to the same identifier collide, which is
// already a hard error. What survives is a leading digit, rejected by [checkIdentifier]
// so that `+1` still asks the author for a name rather than being silently mangled.
func camel(seg string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(seg, isSeparator) {
		r := []rune(w)
		b.WriteString(strings.ToUpper(string(r[0])))
		b.WriteString(string(r[1:]))
	}
	return b.String()
}

func isSeparator(r rune) bool {
	return r == '_' || !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func checkIdentifier(name string) error {
	if !token.IsIdentifier(name) {
		return errors.Errorf("%q is not a Go identifier; set %s", name, NameExtension)
	}
	if token.IsKeyword(name) {
		return errors.Errorf("%q is a Go keyword; set %s", name, NameExtension)
	}
	return nil
}

// unescapePointer decodes the two JSON Pointer escapes (RFC 6901 §3), so a property
// literally named `a/b` is one segment rather than two.
func unescapePointer(seg string) string {
	if !strings.Contains(seg, "~") {
		return seg
	}
	// ~1 before ~0, so an encoded `~1` does not become `/`.
	return strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
}
