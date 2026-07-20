package frontend

import (
	"net/url"
	"strings"

	"github.com/go-faster/errors"
)

// resolveURI resolves ref against base per RFC 3986 reference resolution. An empty base
// leaves ref untouched (beyond normalization), which is the common case for a standalone
// schema loaded without a baseURI.
func resolveURI(base, ref string) (string, error) {
	if ref == "" {
		return base, nil
	}
	if base == "" {
		u, err := url.Parse(ref)
		if err != nil {
			return "", errors.Wrapf(err, "parse %q", ref)
		}
		return u.String(), nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", errors.Wrapf(err, "parse base %q", base)
	}
	r, err := b.Parse(ref)
	if err != nil {
		return "", errors.Wrapf(err, "resolve %q against %q", ref, base)
	}
	return r.String(), nil
}

// splitFragment splits an absolute URI reference into its base (no fragment) and
// fragment parts. The fragment is percent-decoded by url.Parse.
func splitFragment(u string) (base, fragment string, err error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", "", errors.Wrapf(err, "parse %q", u)
	}
	fragment = parsed.Fragment
	parsed.Fragment = ""
	return parsed.String(), fragment, nil
}

// resolveAll resolves every recorded `$ref` to its target [Node], per design §10.1: base
// URI resolution, then a JSON Pointer fragment (`#/...`) or a plain-name `$anchor`
// fragment (`#name`), or the bare resource root when there is no fragment.
//
// `$dynamicRef` is intentionally left unresolved here (Node.Resolved stays nil for it):
// its target depends on the dynamic scope accumulated at evaluation time, which later
// phases own (design §10.2). The `$dynamicAnchor` graph is still exposed via
// [Registry.DynamicAnchor] so those phases don't need to re-walk the document.
// resolveAll never fails on a dangling `$ref`: an unresolvable reference is recorded in
// st.unresolved and left with Resolved == nil, so the rest of the document still compiles
// (design §25 favors diagnostics over aborting). Only genuinely malformed input aborts
// loading earlier, before this pass.
func (st *convState) resolveAll() {
	for n, baseURI := range st.refBaseURI {
		target, err := st.resolveRef(n.Ref, baseURI)
		if err != nil {
			st.unresolved = append(st.unresolved, UnresolvedRef{
				Pointer: n.Pointer,
				Ref:     n.Ref,
				Reason:  err.Error(),
			})
			continue
		}
		n.Resolved = target
		st.addEdge(n, target, false)
	}
}

func (st *convState) resolveRef(ref, baseURI string) (*Node, error) {
	abs, err := resolveURI(baseURI, ref)
	if err != nil {
		return nil, err
	}
	targetBase, fragment, err := splitFragment(abs)
	if err != nil {
		return nil, err
	}

	switch {
	case fragment == "":
		n, ok := st.reg.resources[targetBase]
		if !ok {
			return nil, errors.Errorf("resource %q not found", targetBase)
		}
		return n, nil
	case strings.HasPrefix(fragment, "/"):
		n, ok := st.reg.pointers[targetBase+"\x00"+fragment]
		if !ok {
			return nil, errors.Errorf("pointer %q not found in resource %q", fragment, targetBase)
		}
		return n, nil
	default:
		n, ok := st.reg.anchors[targetBase+"#"+fragment]
		if !ok {
			return nil, errors.Errorf("anchor %q not found in resource %q", fragment, targetBase)
		}
		return n, nil
	}
}
