package frontend

import (
	"context"
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
// External/remote references — those whose target document is not already in the registry —
// are fetched on demand via the configured [Loader]. Because a loaded document may itself
// carry further external refs (and may cycle back), resolution runs as a worklist to a
// fixed point: resolve what is resolvable, load the newly-discovered target documents,
// repeat until nothing new loads. [Registry.analyzeSCCs] runs only afterwards (in
// convertRoot), so cross-document recursion classifies correctly.
//
// `$dynamicRef` is intentionally left unresolved here (Node.Resolved stays nil for it):
// its target depends on the dynamic scope accumulated at evaluation time, which later
// phases own (design §10.2). The `$dynamicAnchor` graph is still exposed via
// [Registry.DynamicAnchor] so those phases don't need to re-walk the document.
// resolveAll never fails on a dangling `$ref`: an unresolvable reference is recorded in
// st.unresolved and left with Resolved == nil, so the rest of the document still compiles
// (design §25 favors diagnostics over aborting). Only genuinely malformed input aborts
// loading earlier, before this pass.
func (st *convState) resolveAll(ctx context.Context) {
	for {
		missing := st.resolvePass()
		if len(missing) == 0 {
			break
		}
		loadedAny := false
		for _, base := range missing {
			if st.tryLoad(ctx, base) {
				loadedAny = true
			}
		}
		if !loadedAny {
			break
		}
	}
	st.finalizeUnresolved()
}

// resolvePass resolves every ref currently resolvable and returns the distinct external
// resource base URIs that are absent from the registry and not yet attempted — the
// documents worth loading before the next pass. It records neither successes as diagnostics
// nor failures as unresolved; that is deferred to finalizeUnresolved after the fixed point.
func (st *convState) resolvePass() []string {
	var missing []string
	seen := make(map[string]bool)
	for n, baseURI := range st.refBaseURI {
		if n.Resolved != nil {
			continue
		}
		targetBase, fragment, err := refLocation(baseURI, n.Ref)
		if err != nil {
			continue
		}
		if target, err := st.lookup(targetBase, fragment); err == nil {
			n.Resolved = target
			st.addEdge(n, target, false)
			continue
		}
		// A present resource with a missing fragment is a genuine dangling ref, not a
		// document to fetch; only a wholly-absent target document is a load candidate.
		if _, ok := st.reg.resources[targetBase]; ok {
			continue
		}
		if st.loaded[targetBase] || seen[targetBase] {
			continue
		}
		seen[targetBase] = true
		missing = append(missing, targetBase)
	}
	return missing
}

// finalizeUnresolved records every ref still lacking a target as an [UnresolvedRef],
// folding in any loader error that explains why its target document is absent.
func (st *convState) finalizeUnresolved() {
	for n, baseURI := range st.refBaseURI {
		if n.Resolved != nil {
			continue
		}
		var reason string
		targetBase, fragment, err := refLocation(baseURI, n.Ref)
		if err != nil {
			reason = err.Error()
		} else if _, lerr := st.lookup(targetBase, fragment); lerr != nil {
			reason = lerr.Error()
			if le, ok := st.loadErrs[targetBase]; ok {
				reason += ": " + le.Error()
			}
		}
		st.unresolved = append(st.unresolved, UnresolvedRef{
			Pointer: n.Pointer,
			Ref:     n.Ref,
			Reason:  reason,
		})
	}
}

// refLocation resolves ref against baseURI (RFC 3986) and splits it into the target
// document's base URI and the fragment.
func refLocation(baseURI, ref string) (targetBase, fragment string, err error) {
	abs, err := resolveURI(baseURI, ref)
	if err != nil {
		return "", "", err
	}
	return splitFragment(abs)
}

// lookup finds the [Node] addressed by a target base URI and fragment within the registry:
// the resource root (empty fragment), a JSON Pointer (`/...`), or a plain-name `$anchor`.
func (st *convState) lookup(targetBase, fragment string) (*Node, error) {
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
