package schemacompiler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// Every soundness bug in the 2026-08-21 review round had one shape: a keyword reached the
// plan only when some other keyword was absent (issue #60). `format` vanished once the
// guard widened past strings (#32); `const` vanished once a `type` was declared beside it
// (#51); `nullable` vanished once applicators appeared without one (#30). Each was found by
// probing, one at a time, and each was correct on the path the golden files happened to
// cover.
//
// This is the cross-product those probes were sampling. A keyword is compiled under every
// sibling and at every position the planner reaches a leaf from, and the plan is asked to
// judge an instance the keyword alone rejects. The question is behavioral, not structural:
// asserting that a constraint appears *somewhere* in the plan means naming every shape it
// may take, which is how the asymmetry got missed in the first place. Interpreting the plan
// asks instead whether anything at all still rejects the value, which is what design §24
// actually requires and is the one direction that is never allowed to fail.
//
// The span is the whole pipeline, so a keyword dropped in the frontend fails here exactly
// like one dropped in the planner — #30 was a frontend bug and #51 a planner bug, and
// neither could hide from this.

// keywordProbe is one keyword and an instance that keyword alone rejects. guard is the
// kind set the keyword applies to; an empty guard means it constrains every kind.
type keywordProbe struct {
	name    string
	body    string
	witness string
	guard   plan.KindSet
}

var keywordProbes = []keywordProbe{
	{"minLength", `"minLength":2`, `"a"`, plan.SetString},
	{"maxLength", `"maxLength":1`, `"ab"`, plan.SetString},
	{"pattern", `"pattern":"^a"`, `"b"`, plan.SetString},
	{"minimum", `"minimum":5`, `1`, plan.SetNumber},
	{"maximum", `"maximum":5`, `9`, plan.SetNumber},
	{"exclusiveMinimum", `"exclusiveMinimum":5`, `5`, plan.SetNumber},
	{"exclusiveMaximum", `"exclusiveMaximum":5`, `5`, plan.SetNumber},
	{"multipleOf", `"multipleOf":3`, `4`, plan.SetNumber},
	{"minItems", `"minItems":2`, `[1]`, plan.SetArray},
	{"maxItems", `"maxItems":1`, `[1,2]`, plan.SetArray},
	{"uniqueItems", `"uniqueItems":true`, `[1,1]`, plan.SetArray},
	{"contains", `"contains":{"const":1}`, `[2]`, plan.SetArray},
	{"minContains", `"contains":{"const":1},"minContains":2`, `[1]`, plan.SetArray},
	{"maxContains", `"contains":{"const":1},"maxContains":1`, `[1,1]`, plan.SetArray},
	{"items", `"items":{"type":"string"}`, `[1]`, plan.SetArray},
	{"prefixItems", `"prefixItems":[{"type":"string"}]`, `[1]`, plan.SetArray},
	{"minProperties", `"minProperties":2`, `{"a":1}`, plan.SetObject},
	{"maxProperties", `"maxProperties":1`, `{"a":1,"b":2}`, plan.SetObject},
	{"required", `"required":["a"]`, `{}`, plan.SetObject},
	{"dependentRequired", `"dependentRequired":{"a":["b"]}`, `{"a":1}`, plan.SetObject},
	{"dependentSchemas", `"dependentSchemas":{"a":{"required":["b"]}}`, `{"a":1}`, plan.SetObject},
	{"propertyNames", `"propertyNames":{"maxLength":1}`, `{"ab":1}`, plan.SetObject},
	{"properties", `"properties":{"a":{"type":"string"}}`, `{"a":1}`, plan.SetObject},
	{"patternProperties", `"patternProperties":{"^a":{"type":"string"}}`, `{"ab":1}`, plan.SetObject},
	{"additionalProperties", `"properties":{"a":{}},"additionalProperties":false`, `{"x":1}`, plan.SetObject},
	{"const-string", `"const":"x"`, `"y"`, 0},
	{"const-object", `"const":{"a":1}`, `{"zzz":9}`, 0},
	{"const-array", `"const":[1]`, `[2]`, 0},
	{"enum-string", `"enum":["x"]`, `"y"`, 0},
	{"enum-object", `"enum":[{"a":1}]`, `{"zzz":9}`, 0},
	{"not", `"not":{"const":"y"}`, `"y"`, 0},
	{"allOf", `"allOf":[{"minLength":2}]`, `"a"`, 0},
	{"anyOf", `"anyOf":[{"minLength":2}]`, `"a"`, 0},
	{"oneOf", `"oneOf":[{"minLength":2}]`, `"a"`, 0},
	{"if-then", `"if":{"minLength":1},"then":{"minLength":3}`, `"ab"`, 0},
}

// keywordSiblings is the axis the confirmed instances moved along: what else is declared in
// the same schema object. Each `type` spelling routes the planner through a different leaf
// builder, and the applicator entries stand in for the paths that lift a keyword out of the
// conjunction and have to put it back (`$ref`, `allOf`, `anyOf`, `oneOf`, `not`).
var keywordSiblings = []struct{ name, body string }{
	{"none", ``},
	{"type-null", `"type":"null"`},
	{"type-boolean", `"type":"boolean"`},
	{"type-number", `"type":"number"`},
	{"type-integer", `"type":"integer"`},
	{"type-string", `"type":"string"`},
	{"type-array", `"type":"array"`},
	{"type-object", `"type":"object"`},
	{"type-multi", `"type":["string","number","object","array","null","boolean"]`},
	{"ref-any", `"$ref":"#/$defs/any"`},
	{"allOf-any", `"allOf":[{}]`},
	{"anyOf-any", `"anyOf":[{},{"const":"zzz-unreachable"}]`},
	{"oneOf-kinds", `"oneOf":[{"type":["string","number","boolean","null"]},{"type":["object","array"]}]`},
	{"not-marker", `"not":{"const":"zzz-unreachable"}`},
	{"nullable", `"nullable":true`},
	{"title", `"title":"t"`},
	{"unknown", `"x-zzz":1`},
}

// keywordPlacements is the third axis: where the probed schema sits. Both templates take
// the schema and the instance respectively; the schema carries its own `$id`, so the
// pointers a probe writes resolve inside it wherever it is nested.
var keywordPlacements = []struct{ name, schema, witness string }{
	{"root", `%s`, `%s`},
	{"property", `{"type":"object","properties":{"p":%s}}`, `{"p":%s}`},
	{"additionalProperties", `{"type":"object","additionalProperties":%s}`, `{"p":%s}`},
	{"patternProperties", `{"type":"object","patternProperties":{"^p$":%s}}`, `{"p":%s}`},
	{"items", `{"type":"array","items":%s}`, `[%s]`},
	{"prefixItems", `{"type":"array","prefixItems":[%s]}`, `[%s]`},
	{"allOf", `{"allOf":[%s]}`, `%s`},
	{"anyOf", `{"anyOf":[%s]}`, `%s`},
	{"oneOf", `{"oneOf":[%s]}`, `%s`},
	{"ref", `{"$ref":"#/$defs/t","$defs":{"t":%s}}`, `%s`},
	{"ref-sibling", `{"$defs":{"any":{}},"allOf":[{"$ref":"#/$defs/any"},%s]}`, `%s`},
	{"double-negation", `{"not":{"not":%s}}`, `%s`},
	{"then", `{"if":{},"then":%s}`, `%s`},
	{"dependentSchemas", `{"dependentSchemas":{"p":{"properties":{"q":%s}}}}`, `{"p":1,"q":%s}`},
}

// TestCompileKeywordMatrixEnforces is the direction §24 never permits to fail: whatever
// else is declared beside a keyword and wherever the schema sits, an instance the keyword
// rejects must not be accepted in silence. Over-approximating is allowed — but only
// declared, so a plan that carries a [plan.DiagnosticUnenforced] or
// [plan.DiagnosticUnsupported], or an interpretation the interpreter could not complete
// exactly, is excused. Silence is not.
func TestCompileKeywordMatrixEnforces(t *testing.T) {
	eachCell(t, func(t *testing.T, schema, witness string) {
		res, verdict := judge(t, schema, witness)
		if verdict.Accepted && !approximated(res, verdict) {
			t.Fatalf("constraint dropped: %s accepts %s with no diagnostic", schema, witness)
		}
	})
}

// TestCompileKeywordMatrixStaysVacuous is the other half of the same asymmetry, and the
// half #32 broke: a kind-guarded keyword says nothing about an instance of another kind,
// so widening its guard rejects values the schema accepts. `{"format":"uuid"}` rejecting
// the number 1 was this, and rejecting a valid instance is the failure §24 rules out
// outright.
func TestCompileKeywordMatrixStaysVacuous(t *testing.T) {
	for _, p := range keywordProbes {
		if p.guard == 0 {
			continue // applies to every kind: no instance it is vacuous for.
		}
		for _, other := range vacuousWitnesses(p.guard) {
			for _, pl := range keywordPlacements {
				schema := fmt.Sprintf(pl.schema, probeSchema(p.body))
				witness := fmt.Sprintf(pl.witness, other)
				t.Run(fmt.Sprintf("%s/%s/%s", p.name, other, pl.name), func(t *testing.T) {
					_, verdict := judge(t, schema, witness)
					if !verdict.Accepted {
						t.Fatalf("valid instance rejected: %s rejects %s: %v", schema, witness, verdict.Reason)
					}
				})
			}
		}
	}
}

// vacuousWitnesses returns one instance of each kind outside guard.
func vacuousWitnesses(guard plan.KindSet) []string {
	byKind := map[plan.JSONKind]string{
		plan.KindNull: `null`, plan.KindBoolean: `true`, plan.KindNumber: `1`,
		plan.KindString: `"zz"`, plan.KindArray: `[]`, plan.KindObject: `{}`,
	}
	var out []string
	for kind, witness := range byKind {
		if !guard.Has(kind) {
			out = append(out, witness)
		}
	}
	sort.Strings(out)
	return out
}

// eachCell walks the keyword x sibling x placement cross-product. A sibling that spells the
// same keyword the probe does is skipped: the two would collide in one JSON object, and
// which one survived would be the parser's answer rather than the compiler's.
func eachCell(t *testing.T, check func(t *testing.T, schema, witness string)) {
	t.Helper()
	for _, p := range keywordProbes {
		for _, sib := range keywordSiblings {
			if collides(p.body, sib.body) {
				continue
			}
			body := p.body
			if sib.body != "" {
				body = sib.body + "," + body
			}
			for _, pl := range keywordPlacements {
				schema := fmt.Sprintf(pl.schema, probeSchema(body))
				witness := fmt.Sprintf(pl.witness, p.witness)
				t.Run(fmt.Sprintf("%s/%s/%s", p.name, sib.name, pl.name), func(t *testing.T) {
					check(t, schema, witness)
				})
			}
		}
	}
}

// probeSchema closes the probed body into a schema resource: the `$id` scopes the pointers
// in it to itself, so the same body compiles unchanged at every placement.
func probeSchema(body string) string {
	return `{"$id":"urn:probe","$defs":{"any":{}},` + body + `}`
}

func collides(body, sibling string) bool {
	if sibling == "" {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(sibling, `"`), `"`)
	return strings.Contains(body, `"`+name+`"`)
}

func judge(t *testing.T, schema, witness string) (*schemacompiler.Result, planterp.Verdict) {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	var v any
	require.NoError(t, json.Unmarshal([]byte(witness), &v))
	verdict, err := planterp.Interpret(res.Plan, v)
	require.NoError(t, err)
	require.Emptyf(t, planwalk.OverbroadGuards(res.Plan),
		"a guard fires on a kind its predicate cannot read, in %s", schema)
	return res, verdict
}

// approximated reports whether the plan declared that it accepts more than the schema does.
func approximated(res *schemacompiler.Result, verdict planterp.Verdict) bool {
	if len(verdict.Approximated) > 0 {
		return true
	}
	for _, d := range res.Diagnostics {
		if d.Kind == plan.DiagnosticUnenforced || d.Kind == plan.DiagnosticUnsupported {
			return true
		}
	}
	return false
}
