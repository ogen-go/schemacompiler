package frontend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func uninhabitedPointers(s *Schema) []string {
	ps := make([]string, len(s.Uninhabited))
	for i, u := range s.Uninhabited {
		ps[i] = u.Pointer
	}
	return ps
}

func TestInhabit_RequiredSelfRecursionUninhabited(t *testing.T) {
	// {type:object, required:[self], properties:{self:{$ref:A}}} has no finite instance:
	// every value must contain a strictly smaller one of itself (issue #8).
	doc := `{
		"$defs": {
			"A": {
				"type": "object",
				"required": ["self"],
				"properties": {"self": {"$ref": "#/$defs/A"}}
			}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.Contains(t, uninhabitedPointers(s), "/$defs/A")
}

func TestInhabit_MutualRequiredRecursionUninhabited(t *testing.T) {
	// A requires b:B, B requires a:A; both object-only, so neither has a finite instance.
	doc := `{
		"$defs": {
			"A": {"type": "object", "required": ["b"], "properties": {"b": {"$ref": "#/$defs/B"}}},
			"B": {"type": "object", "required": ["a"], "properties": {"a": {"$ref": "#/$defs/A"}}}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.ElementsMatch(t, []string{"/$defs/A", "/$defs/B"}, uninhabitedPointers(s))
}

func TestInhabit_OptionalRecursionInhabited(t *testing.T) {
	// The self property is optional: `{}` inhabits the schema, so it is not flagged.
	doc := `{
		"$defs": {
			"A": {"type": "object", "properties": {"self": {"$ref": "#/$defs/A"}}}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.Empty(t, s.Uninhabited)
}

func TestInhabit_NullableRecursionInhabited(t *testing.T) {
	// The recursive node also accepts null, so `null` is a finite witness — not flagged.
	doc := `{
		"$defs": {
			"A": {
				"type": ["object", "null"],
				"required": ["self"],
				"properties": {"self": {"$ref": "#/$defs/A"}}
			}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.Empty(t, s.Uninhabited)
}

func TestInhabit_UntypedRequiredRecursionInhabited(t *testing.T) {
	// Without a `type` restriction, `required` is vacuous for a non-object instance (a
	// scalar witness satisfies the schema), so the recursion is inhabited.
	doc := `{
		"$defs": {
			"A": {"required": ["self"], "properties": {"self": {"$ref": "#/$defs/A"}}}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.Empty(t, s.Uninhabited)
}

func TestInhabit_NullableBranchEscapeInhabited(t *testing.T) {
	// The required property may be null (via a nested combinator), which the analysis does
	// not dissect but conservatively treats as inhabited — no false positive.
	doc := `{
		"$defs": {
			"A": {
				"type": "object",
				"required": ["self"],
				"properties": {"self": {"anyOf": [{"$ref": "#/$defs/A"}, {"type": "null"}]}}
			}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	require.Empty(t, s.Uninhabited)
}

func TestInhabit_NonRecursiveNotReported(t *testing.T) {
	// A finitely-nested required chain terminates and is inhabited; nothing is flagged.
	doc := `{
		"type": "object",
		"required": ["a"],
		"properties": {
			"a": {"type": "object", "required": ["b"], "properties": {"b": {"type": "string"}}}
		}
	}`
	s := mustLoad(t, doc)
	require.Empty(t, s.Uninhabited)
}
