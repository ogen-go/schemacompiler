package frontend

// recordUnusedDiscriminator records a `discriminator` the compiler has no union to attach
// to (issue #46).
//
// OAS 3.0.3 line 2705: "The discriminator object is legal only when using one of the
// composite keywords oneOf, anyOf, allOf." Only `oneOf` and `anyOf` list the alternatives
// in place, and those are the two [Node] positions the semantic compiler carries the
// declaration on. Line 2761 sanctions a second spelling — "the discriminator MAY be added
// to a parent schema definition, and all schemas comprising the parent schema in an allOf
// construct may be used as an alternate schema" — whose alternatives are not in the schema
// at all but scattered across the document, one per schema that includes the parent.
//
// That idiom does not drive dispatch today, so it is reported rather than dropped: an
// author writing the idiomatic OpenAPI spelling would otherwise get no dispatch and no
// explanation.
func (st *convState) recordUnusedDiscriminator(n *Node) {
	if n.Discriminator == nil || len(n.AnyOf) > 0 || len(n.OneOf) > 0 {
		return
	}
	st.unusedDiscriminator = append(st.unusedDiscriminator, UnusedDiscriminator{
		Pointer:      n.Pointer,
		Position:     n.Position,
		PropertyName: n.Discriminator.PropertyName,
	})
}
