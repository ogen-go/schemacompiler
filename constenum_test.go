package schemacompiler_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

// repShape names a representation by its variant, plus the kind for a primitive, so a
// table can pin the shape without spelling out a whole nested representation tree.
func repShape(r plan.Representation) string {
	switch r := r.(type) {
	case plan.AnyRepresentation:
		return "any"
	case plan.NeverRepresentation:
		return "never"
	case plan.PrimitiveRepresentation:
		return "primitive:" + kindName(r.Kind)
	case plan.ObjectRepresentation:
		return "object"
	case plan.ArrayRepresentation:
		return "array"
	case plan.UnionRepresentation:
		out := "union("
		for i, alt := range r.Alternatives {
			if i > 0 {
				out += ","
			}
			out += repShape(alt)
		}
		return out + ")"
	case plan.ReferenceRepresentation:
		return "ref:" + r.Name
	default:
		return fmt.Sprintf("%T", r)
	}
}

func kindName(k plan.JSONKind) string {
	switch k {
	case plan.KindNull:
		return "null"
	case plan.KindBoolean:
		return "boolean"
	case plan.KindNumber:
		return "number"
	case plan.KindString:
		return "string"
	case plan.KindArray:
		return "array"
	case plan.KindObject:
		return "object"
	default:
		return fmt.Sprintf("kind(%d)", k)
	}
}

func dispatchShape(d plan.DispatchPlan) string {
	switch d.(type) {
	case plan.NoDispatch:
		return "none"
	case plan.LiteralDispatch:
		return "literal"
	case plan.KindDispatch:
		return "kind"
	case plan.PropertyDispatch:
		return "property"
	case plan.PresenceDispatch:
		return "presence"
	case plan.PredicateCountDispatch:
		return "predicate-count"
	default:
		return fmt.Sprintf("%T", d)
	}
}

// literalCaseValues returns the raw JSON source of each LiteralDispatch case, in order,
// or nil when the plan does not dispatch on literals.
func literalCaseValues(d plan.DispatchPlan) []string {
	ld, ok := d.(plan.LiteralDispatch)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(ld.Cases))
	for _, c := range ld.Cases {
		out = append(out, string(c.Raw))
	}
	return out
}

// TestCompileConstEnum pins how `const` and `enum` lower, across value kinds and with or
// without a sibling `type` (design §11.3, §11.4). Issues #51, #52, #53.
func TestCompileConstEnum(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		rep        string
		dispatch   string
		literals   []string
		capability plan.CapabilityLevel
		exactness  plan.Exactness
	}{
		// const, no sibling type.
		{
			name: "const string", schema: `{"const":"x"}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"x"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const number", schema: `{"const":3}`,
			rep: "primitive:number", dispatch: "literal", literals: []string{`3`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const null", schema: `{"const":null}`,
			rep: "primitive:null", dispatch: "literal", literals: []string{`null`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const object", schema: `{"const":{"a":1}}`,
			rep: "object", dispatch: "literal", literals: []string{`{"a":1}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const array", schema: `{"const":[1,2]}`,
			rep: "array", dispatch: "literal", literals: []string{`[1,2]`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},

		// const with a sibling type: the literal must survive (issue #51).
		{
			name: "const string with type", schema: `{"type":"string","const":"x"}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"x"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const integer with type", schema: `{"type":"integer","const":3}`,
			rep: "primitive:number", dispatch: "literal", literals: []string{`3`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const null with type", schema: `{"type":"null","const":null}`,
			rep: "primitive:null", dispatch: "literal", literals: []string{`null`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const object with type", schema: `{"type":"object","const":{"a":1}}`,
			rep: "object", dispatch: "literal", literals: []string{`{"a":1}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const array with type", schema: `{"type":"array","const":[1,2]}`,
			rep: "array", dispatch: "literal", literals: []string{`[1,2]`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "const with type array", schema: `{"type":["string","number"],"const":"x"}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"x"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},

		// enum, no sibling type.
		{
			name: "enum strings", schema: `{"enum":["a","b"]}`,
			rep: "union(primitive:string,primitive:string)", dispatch: "literal",
			literals: []string{`"a"`, `"b"`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum booleans", schema: `{"enum":[true,false]}`,
			rep: "union(primitive:boolean,primitive:boolean)", dispatch: "literal",
			literals: []string{`true`, `false`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum single object", schema: `{"enum":[{"a":1}]}`,
			rep: "object", dispatch: "literal", literals: []string{`{"a":1}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},

		// enum with a sibling type: every member must survive (issue #51).
		{
			name: "enum single string with type", schema: `{"type":"string","enum":["a"]}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"a"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum single object with type", schema: `{"type":"object","enum":[{"a":1}]}`,
			rep: "object", dispatch: "literal", literals: []string{`{"a":1}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum single array with type", schema: `{"type":"array","enum":[[1]]}`,
			rep: "array", dispatch: "literal", literals: []string{`[1]`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum single null with type", schema: `{"type":"null","enum":[null]}`,
			rep: "primitive:null", dispatch: "literal", literals: []string{`null`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum objects with type", schema: `{"type":"object","enum":[{"a":1},{"a":2}]}`,
			rep: "union(object,object)", dispatch: "literal",
			literals: []string{`{"a":1}`, `{"a":2}`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum arrays with type", schema: `{"type":"array","enum":[[1],[2]]}`,
			rep: "union(array,array)", dispatch: "literal",
			literals: []string{`[1]`, `[2]`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum with type array", schema: `{"type":["string","null"],"enum":["a",null]}`,
			rep: "union(primitive:string,primitive:null)", dispatch: "literal",
			literals: []string{`"a"`, `null`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},

		// enum members that are JSON-equal are one value (issue #53).
		{
			name: "enum numbers equal across notation", schema: `{"enum":[1,1.0]}`,
			rep: "primitive:number", dispatch: "literal", literals: []string{`1`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum numbers equal across exponent", schema: `{"enum":[100,1e2]}`,
			rep: "primitive:number", dispatch: "literal", literals: []string{`100`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum objects equal across member order", schema: `{"enum":[{"a":1,"b":2},{"b":2,"a":1}]}`,
			rep: "object", dispatch: "literal", literals: []string{`{"a":1,"b":2}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name:   "enum objects equal across nested number notation",
			schema: `{"type":"object","enum":[{"a":1},{"a":1.0}]}`,
			rep:    "object", dispatch: "literal", literals: []string{`{"a":1}`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum duplicate strings", schema: `{"enum":["a","a"]}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"a"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			// Arrays are order-sensitive, so these are two distinct values.
			name: "enum arrays differing in element order", schema: `{"enum":[[1,2],[2,1]]}`,
			rep: "union(array,array)", dispatch: "literal",
			literals: []string{`[1,2]`, `[2,1]`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			// Beyond float64 precision: mathematically distinct, and must stay distinct.
			name:   "enum integers beyond float64 precision",
			schema: `{"enum":[10000000000000000000000001,10000000000000000000000002]}`,
			rep:    "union(primitive:number,primitive:number)", dispatch: "literal",
			literals:   []string{`10000000000000000000000001`, `10000000000000000000000002`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},

		{
			name:   "enum decimals beyond float64 precision",
			schema: `{"enum":[0.10000000000000000000000001,0.10000000000000000000000002]}`,
			rep:    "union(primitive:number,primitive:number)", dispatch: "literal",
			literals:   []string{`0.10000000000000000000000001`, `0.10000000000000000000000002`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			// oneOf over two spellings of one value is ExactlyOne(A, A) == Never
			// (design §15.1), which only JSON-value equality can see.
			name: "oneOf over json-equal consts", schema: `{"oneOf":[{"const":1},{"const":1.0}]}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},

		// A declared-but-empty enum accepts nothing (issue #52).
		{
			name: "empty enum", schema: `{"enum":[]}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},
		{
			name: "empty enum with type", schema: `{"type":"string","enum":[]}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},

		// A const whose kind the sibling type excludes accepts nothing.
		{
			name: "const excluded by type", schema: `{"type":"string","const":1}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},

		// An enum member the sibling type excludes is dead and must be dropped, not kept
		// as a Never dispatch case (design §15.5, §16.2; issue #59).
		{
			name: "enum no members excluded by type", schema: `{"type":"string","enum":["a","b"]}`,
			rep: "union(primitive:string,primitive:string)", dispatch: "literal",
			literals: []string{`"a"`, `"b"`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum one member excluded by type", schema: `{"type":"string","enum":["a",1]}`,
			rep: "primitive:string", dispatch: "literal", literals: []string{`"a"`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum some members excluded by type", schema: `{"type":"string","enum":["a",1,true,"b",null]}`,
			rep: "union(primitive:string,primitive:string)", dispatch: "literal",
			literals: []string{`"a"`, `"b"`}, capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum members excluded by integer type", schema: `{"type":"integer","enum":[1,"a"]}`,
			rep: "primitive:number", dispatch: "literal", literals: []string{`1`},
			capability: plan.StaticDispatch, exactness: plan.ExactWithValidation,
		},
		{
			name: "enum all members excluded by type", schema: `{"type":"string","enum":[1,2]}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},
		{
			name: "enum all members excluded by object type", schema: `{"type":"object","enum":["a",1]}`,
			rep: "never", dispatch: "none", capability: plan.DirectGoType,
			exactness: plan.ExactPureRepresentation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.rep, repShape(res.Plan.Representation), "representation")
			require.Equal(t, tt.dispatch, dispatchShape(res.Plan.Dispatch), "dispatch")
			require.Equal(t, tt.literals, literalCaseValues(res.Plan.Dispatch), "literal cases")
			require.Equal(t, tt.capability, res.Capability, "capability")
			require.Equal(t, tt.exactness, res.Exactness, "exactness")
			require.Empty(t, res.Plan.Validation.Predicates, "residual validation")
			for _, d := range res.Diagnostics {
				require.NotEqual(t, plan.SeverityWarning, d.Severity, "unexpected diagnostic: %s", d.Message)
				require.NotEqual(t, plan.SeverityError, d.Severity, "unexpected diagnostic: %s", d.Message)
			}
		})
	}
}

// TestConstEnumSpellingsAgree pins that a single-value `const`/`enum` compiles to the
// same representation whether or not a redundant sibling `type` is written, for every
// value kind (issue #58): a literal never yields a PrimitiveRepresentation for an object
// or array value, which docs/integration.md §1 reserves for Go scalars.
func TestConstEnumSpellingsAgree(t *testing.T) {
	for _, tt := range []struct {
		name  string
		typ   string
		value string
		rep   string
	}{
		{"null", "null", `null`, "primitive:null"},
		{"boolean", "boolean", `true`, "primitive:boolean"},
		{"number", "number", `3`, "primitive:number"},
		{"string", "string", `"x"`, "primitive:string"},
		{"array", "array", `[1,2]`, "array"},
		{"object", "object", `{"a":1}`, "object"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, spelling := range []string{
				fmt.Sprintf(`{"const":%s}`, tt.value),
				fmt.Sprintf(`{"type":%q,"const":%s}`, tt.typ, tt.value),
				fmt.Sprintf(`{"enum":[%s]}`, tt.value),
				fmt.Sprintf(`{"type":%q,"enum":[%s]}`, tt.typ, tt.value),
			} {
				res, err := schemacompiler.Compile(context.Background(), []byte(spelling), schemacompiler.Options{})
				require.NoError(t, err, spelling)
				require.Equal(t, tt.rep, repShape(res.Plan.Representation), spelling)
				require.Equal(t, "literal", dispatchShape(res.Plan.Dispatch), spelling)
				require.Equal(t, []string{tt.value}, literalCaseValues(res.Plan.Dispatch), spelling)
				require.Equal(t, plan.StaticDispatch, res.Capability, spelling)
			}
		})
	}
}

// TestOverlapDiagnosticVocabulary pins that a diagnostic emitted from an IR node
// describes the IR in its own terms and never names source syntax the author may not have
// written (issue #54): `enum`, `if`/`then`/`else` and `dependentSchemas` all lower to
// ir.AnyOf, and if/then/else and dependentSchemas synthesize an ir.Not, so neither message
// may claim the schema contains oneOf, anyOf or not.
func TestOverlapDiagnosticVocabulary(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		overlap bool
	}{
		{name: "oneOf", schema: `{"oneOf":[{"minimum":0},{"maximum":10}]}`, overlap: true},
		{name: "anyOf", schema: `{"anyOf":[{"minimum":0},{"maximum":10}]}`, overlap: true},
		{
			name:   "if/then/else",
			schema: `{"if":{"minimum":0},"then":{"maximum":5},"else":{"maximum":100}}`, overlap: true,
		},
		{name: "json-equal enum no longer overlaps", schema: `{"enum":[1,1.0]}`},
		{name: "one dependentSchemas entry", schema: `{"dependentSchemas":{"a":{"required":["b"]}}}`},
		{
			// Two entries do overlap, where one does not: both messages fire here.
			name:    "two dependentSchemas entries",
			schema:  `{"dependentSchemas":{"a":{"required":["b"]},"c":{"required":["d"]}}}`,
			overlap: true,
		},
		{name: "type array", schema: `{"type":["string","number"]}`},
		{name: "not", schema: `{"not":{"minimum":0}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			found := false
			for _, d := range res.Diagnostics {
				require.NotContains(t, d.Message, "oneOf", "diagnostic names source syntax")
				require.NotContains(t, d.Message, "anyOf", "diagnostic names source syntax")
				require.NotContains(t, d.Message, "not:", "diagnostic names source syntax")
				if d.Message == "union alternatives overlap; requires predicate-count dispatch" {
					found = true
					require.Equal(t, plan.SeverityWarning, d.Severity)
				}
			}
			require.Equal(t, tt.overlap, found, "overlap diagnostic")
		})
	}
}
