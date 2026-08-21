package frontend

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// applyNullable folds the OpenAPI 3.0 `nullable` keyword into n (issue #20).
//
// OAS 3.0.3 line 2335 defines it narrowly: a true value "adds 'null' to the allowed type
// specified by the `type` keyword, only if `type` is explicitly defined within the same
// Schema Object. Other Schema Object constraints retain their defined behavior, and
// therefore may disallow the use of null as a value."
//
// Both clauses bind. Without a sibling `type` there is nothing to widen, so the keyword is
// ignored — reported through [Schema.IgnoredNullable] rather than dropped silently, since
// an author writing it on a `$ref` or a combinator almost certainly meant a null union.
// With one, the widening applies to that keyword alone: `enum`, `const`, `oneOf` and every
// other sibling keep their own behavior and may go on rejecting null.
//
// authored must be the Schema Object the author wrote at n's position, which at a `$ref`
// is not the schema convertSchema was handed — see [convState.authoredSchema].
func (st *convState) applyNullable(n *Node, authored *base.Schema) {
	if authored == nil || authored.Nullable == nil || !*authored.Nullable {
		return
	}
	if len(authored.Type) == 0 {
		st.ignoredNullable = append(st.ignoredNullable, IgnoredNullable{
			Pointer:  n.Pointer,
			Position: n.Position,
		})
		return
	}
	n.Types |= KindNull
}

// authoredSchema returns the Schema Object declared at hs's position, which is hs itself
// everywhere except a `$ref`: libopenapi resolves a reference proxy to its *target*, so
// convertSchema is handed the target's keywords there. `nullable` and `type` declared
// beside the reference are only visible on the node that declared it, and reading them off
// the target instead would attribute the target's keywords to the referring schema.
func (st *convState) authoredSchema(ctx context.Context, hs *base.Schema) (*base.Schema, error) {
	if hs == nil || hs.ParentProxy == nil || !hs.ParentProxy.IsReference() {
		return hs, nil
	}
	return st.siblingSchema(ctx, hs.ParentProxy.GetReferenceNode())
}
