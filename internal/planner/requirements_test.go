package planner_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

func details(locs []plan.Location) []string {
	out := make([]string, 0, len(locs))
	for _, l := range locs {
		out = append(out, l.Detail)
	}
	return out
}

// TestRequireECMARegex pins which patterns are reported as needing a real ECMA-262 engine
// (design §11.10, §25). The test is one-directional on purpose: a pattern RE2 reads the
// same way must not be listed, since a field that fires for everything trains a consumer
// to ignore it, while a pattern that *may* differ is listed even though whether it
// actually does is undecidable.
func TestRequireECMARegex(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		required bool
	}{
		{name: "plain literal", pattern: "^abc$", required: false},
		{name: "ordinary class", pattern: "^[a-z]+$", required: false},
		{name: "explicit whitespace class", pattern: `^[\t ]$`, required: false},
		{name: "lookbehind RE2 cannot compile", pattern: `(?<=a)b`, required: true},
		{name: "backreference RE2 cannot compile", pattern: `^(a)\1$`, required: true},
		{name: "control escape RE2 cannot compile", pattern: `^\cC$`, required: true},
		{name: "whitespace shorthand differs", pattern: `^\s$`, required: true},
		{name: "digit shorthand differs", pattern: `^\d$`, required: true},
		{name: "word shorthand differs", pattern: `^\w$`, required: true},
		{name: "negated shorthand differs", pattern: `^\S$`, required: true},
		{name: "unicode property differs", pattern: `^\p{L}$`, required: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(&ir.All{Operands: []ir.Expr{
				&ir.Kinds{Set: plan.SetString},
				&ir.Predicate{Guard: plan.SetString, Detail: &ir.PatternDetail{Regex: tt.pattern}},
			}}, nil)

			if !tt.required {
				require.Empty(t, got.Requirements.ECMARegex)
				return
			}
			require.Equal(t, []string{"pattern " + strconv.Quote(tt.pattern)},
				details(got.Requirements.ECMARegex))
		})
	}
}

// TestRequireUnboundedNumeric pins design §24.2: a numeric slot the schema does not bound
// narrows under any fixed-width Go type and must say so, and one the schema does bound is
// discharged statically and must not be listed.
func TestRequireUnboundedNumeric(t *testing.T) {
	numberWith := func(preds ...ir.Expr) ir.Expr {
		return &ir.All{Operands: append([]ir.Expr{&ir.Kinds{Set: plan.SetNumber}}, preds...)}
	}
	minimum := ir.Predicate{Guard: plan.SetNumber, Detail: &ir.MinimumDetail{Value: 0}}
	maximum := ir.Predicate{Guard: plan.SetNumber, Detail: &ir.MaximumDetail{Value: 1000}}

	tests := []struct {
		name     string
		expr     ir.Expr
		required bool
	}{
		{name: "bare number", expr: numberWith(), required: true},
		{name: "only a lower bound", expr: numberWith(&minimum), required: true},
		{name: "only an upper bound", expr: numberWith(&maximum), required: true},
		{name: "bounded both ways", expr: numberWith(&minimum, &maximum), required: false},
		{name: "a string is not numeric", expr: &ir.Kinds{Set: plan.SetString}, required: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(tt.expr, nil)
			require.Equal(t, tt.required, len(got.Requirements.UnboundedNumeric) == 1,
				"got %v", details(got.Requirements.UnboundedNumeric))
		})
	}
}

// TestRequireRawEvaluation pins the checks that inspect something decoding discards
// (design §24.3), so a consumer cannot discharge them against the decoded value.
func TestRequireRawEvaluation(t *testing.T) {
	object := func(preds ...ir.Expr) ir.Expr {
		return &ir.All{Operands: append([]ir.Expr{&ir.Kinds{Set: plan.SetObject}}, preds...)}
	}
	pred := func(d ir.PredicateDetail) ir.Expr {
		return &ir.Predicate{Guard: plan.SetObject, Detail: d}
	}

	tests := []struct {
		name string
		expr ir.Expr
		want int
	}{
		{name: "minProperties", expr: object(pred(&ir.MinPropertiesDetail{Value: 1})), want: 1},
		{name: "maxProperties", expr: object(pred(&ir.MaxPropertiesDetail{Value: 1})), want: 1},
		{
			name: "propertyNames",
			expr: object(pred(&ir.PropertyNamesDetail{Schema: &ir.Kinds{Set: plan.SetString}})),
			want: 1,
		},
		{
			name: "additionalProperties false",
			expr: object(&ir.Shape{Detail: &ir.ObjectShape{AdditionalProperties: &ir.Never{}}}),
			want: 1,
		},
		{
			name: "uniqueItems",
			expr: &ir.All{Operands: []ir.Expr{
				&ir.Kinds{Set: plan.SetArray},
				&ir.Predicate{Guard: plan.SetArray, Detail: &ir.UniqueItemsDetail{}},
			}},
			want: 1,
		},
		{
			name: "required is discharged by presence, not by the raw document",
			expr: object(pred(&ir.RequiredDetail{Properties: []string{"a"}})),
			want: 0,
		},
		{
			name: "an open object asks nothing",
			expr: object(),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(tt.expr, nil)
			require.Len(t, got.Requirements.RawEvaluation, tt.want,
				"got %v", details(got.Requirements.RawEvaluation))
		})
	}
}

// TestRequireJSONEquality pins that `uniqueItems` is reported as comparing JSON values:
// two JSON-distinct items that decode to the same Go value would make it reject an array
// the schema accepts (design §24.3).
func TestRequireJSONEquality(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetArray},
		&ir.Predicate{Guard: plan.SetArray, Detail: &ir.UniqueItemsDetail{}},
	}}, nil)
	require.Equal(t, []string{"uniqueItems"}, details(got.Requirements.JSONEquality))
}

// TestRequirementsAreDeduplicated pins that one keyword is reported once, however often
// the planner passes through it: a `patternProperties` pattern is matched against every
// declared property name.
func TestRequirementsAreDeduplicated(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetObject},
		&ir.Shape{Detail: &ir.ObjectShape{
			Properties: []ir.PropertyExpr{
				{Name: "a", Schema: &ir.Kinds{Set: plan.SetString}},
				{Name: "b", Schema: &ir.Kinds{Set: plan.SetString}},
				{Name: "c", Schema: &ir.Kinds{Set: plan.SetString}},
			},
			PatternProperties: []ir.PatternPropertyExpr{{Pattern: `^\d`, Schema: &ir.Kinds{Set: plan.SetString}}},
		}},
	}}, nil)

	require.Equal(t, []string{`patternProperties "^\\d"`}, details(got.Requirements.ECMARegex))
}

// TestRequirementsEmptyForAPlainSchema keeps the fields from firing on everything: a
// consumer must be able to read an empty Requirements as "nothing special here".
func TestRequirementsEmptyForAPlainSchema(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetObject},
		&ir.Shape{Detail: &ir.ObjectShape{
			Properties: []ir.PropertyExpr{{Name: "a", Schema: &ir.Kinds{Set: plan.SetString}}},
		}},
	}}, nil)
	require.True(t, got.Requirements.Empty(), "got %+v", got.Requirements)
}
