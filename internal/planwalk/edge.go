package planwalk

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/plan"
)

// EdgeKind names the relation a child node has to its parent.
type EdgeKind uint8

const (
	// EdgeRoot is the zero edge, carried by a node built as a fold root rather than
	// reached from a parent.
	EdgeRoot EdgeKind = iota
	// EdgeRepresentation links a plan to its own representation tree.
	EdgeRepresentation
	// EdgeDispatch links a plan to its dispatch.
	EdgeDispatch
	// EdgeValidation links a plan to its residual validation.
	EdgeValidation
	// EdgeField links an object representation to the plan of a declared field's
	// values. Name is the property name; Presence and Nullable are the field's own.
	EdgeField
	// EdgeAdditional links an object representation to the plan of its Additional
	// values, which cover every property no field and no pattern rule covers.
	EdgeAdditional
	// EdgePatternRule links an object representation to the plan of a pattern rule's
	// values. Name is the pattern and Index its position in PatternRules.
	EdgePatternRule
	// EdgePrefixItem links an array representation to the plan of the tuple slot at
	// Index.
	EdgePrefixItem
	// EdgeRestItem links an array representation to the plan of every item past the
	// tuple prefix.
	EdgeRestItem
	// EdgeAlternative links a union representation to the alternative at Index.
	EdgeAlternative
	// EdgeRecursiveBody links a recursive representation to its body. Name is the
	// binder the body's references resolve against.
	EdgeRecursiveBody
	// EdgeKindCase links a kind dispatch to the branch selected for Case.
	EdgeKindCase
	// EdgeLiteralCase links a literal dispatch to the branch selected when the whole
	// instance equals the literal. Value and Raw carry that literal; Index is the
	// case's position.
	EdgeLiteralCase
	// EdgePropertyCase links a property dispatch to the branch selected when the tag
	// property equals the literal. Name is the tag property, Tag its provenance, and
	// Value, Raw and Index are as for EdgeLiteralCase.
	EdgePropertyCase
	// EdgePresent links a presence dispatch to the branch for Name being present.
	EdgePresent
	// EdgeAbsent links a presence dispatch to the branch for Name being absent.
	EdgeAbsent
	// EdgeCountBranch links a predicate-count dispatch to the branch at Index. Every
	// branch is trial-validated, so this edge does not select.
	EdgeCountBranch
	// EdgeGuardedPredicate links a validation plan to the predicate at Index.
	// Applicability is the kind guard the predicate only applies under.
	EdgeGuardedPredicate
	// EdgeContainsSchema links a contains-count predicate to the plan each element is
	// counted against.
	EdgeContainsSchema
	// EdgeNegationSchema links a negation predicate to the plan the instance must
	// NOT satisfy.
	EdgeNegationSchema
	// EdgePropertyNamesSchema links a property-names predicate to the plan each
	// property name is checked against.
	EdgePropertyNamesSchema
)

var edgeKindNames = [...]string{
	EdgeRoot:                "root",
	EdgeRepresentation:      "representation",
	EdgeDispatch:            "dispatch",
	EdgeValidation:          "validation",
	EdgeField:               "field",
	EdgeAdditional:          "additional",
	EdgePatternRule:         "pattern-rule",
	EdgePrefixItem:          "prefix-item",
	EdgeRestItem:            "rest-item",
	EdgeAlternative:         "alternative",
	EdgeRecursiveBody:       "recursive-body",
	EdgeKindCase:            "kind-case",
	EdgeLiteralCase:         "literal-case",
	EdgePropertyCase:        "property-case",
	EdgePresent:             "present",
	EdgeAbsent:              "absent",
	EdgeCountBranch:         "count-branch",
	EdgeGuardedPredicate:    "guarded-predicate",
	EdgeContainsSchema:      "contains-schema",
	EdgeNegationSchema:      "negation-schema",
	EdgePropertyNamesSchema: "property-names-schema",
}

func (k EdgeKind) String() string {
	if int(k) >= len(edgeKindNames) {
		return "edge-kind(" + strconv.Itoa(int(k)) + ")"
	}
	return edgeKindNames[k]
}

// Edge is how a node hangs off its parent: which slot it fills, plus the slot's own
// data — everything the parent structure holds about the child that is not part of the
// child node itself.
//
// It is what lets an instance-directed consumer derive the sub-value a child applies to
// (the value of field Name, the element at Index, the branch the tag selects) without
// re-deriving the plan's shape. Each field documents the kinds it is meaningful for and
// is zero otherwise. Slot [plan.Metadata] is deliberately absent: it describes the
// schema for documentation, never which sub-value a child applies to.
type Edge struct {
	Kind EdgeKind
	// Name is the property name (EdgeField, EdgePropertyCase, EdgePresent, EdgeAbsent),
	// the pattern (EdgePatternRule) or the recursion binder (EdgeRecursiveBody).
	Name string
	// Index is the position within the parent's slice: EdgePatternRule,
	// EdgePrefixItem, EdgeAlternative, EdgeLiteralCase, EdgePropertyCase,
	// EdgeCountBranch, EdgeGuardedPredicate.
	Index int
	// Case is the dispatched-on kind (EdgeKindCase).
	Case plan.JSONKind
	// Presence and Nullable are the field's own (EdgeField).
	Presence plan.PresenceMode
	Nullable bool
	// Tag is the provenance of the dispatch tag (EdgePropertyCase).
	Tag plan.TagSource
	// Value and Raw are the literal the case is selected by (EdgeLiteralCase,
	// EdgePropertyCase). Raw is the exact source bytes when the plan kept them.
	Value any
	Raw   []byte
	// Applicability is the kind guard the predicate applies under
	// (EdgeGuardedPredicate).
	Applicability plan.KindSet
}
