// Package dump renders compiler pipeline stages (ir.Expr, plan.CompilationPlan) as
// deterministic, human-readable indented text trees, for debugging and golden tests.
package dump

import (
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// kindNames lists every single-kind bit of [plan.KindSet] in a stable order, paired
// with its short display name.
var kindNames = []struct {
	bit  plan.KindSet
	name string
}{
	{plan.SetNull, "null"},
	{plan.SetBoolean, "boolean"},
	{plan.SetNumber, "number"},
	{plan.SetString, "string"},
	{plan.SetArray, "array"},
	{plan.SetObject, "object"},
}

// kindSetString renders a [plan.KindSet] as e.g. "{string,number}", "any", or "never".
func kindSetString(s plan.KindSet) string {
	if s == plan.SetAny {
		return "any"
	}
	if s == 0 {
		return "never"
	}
	var names []string
	for _, k := range kindNames {
		if s&k.bit != 0 {
			names = append(names, k.name)
		}
	}
	return "{" + strings.Join(names, ",") + "}"
}

// jsonKindString renders a single [plan.JSONKind].
func jsonKindString(k plan.JSONKind) string {
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
		return "unknown"
	}
}

// numericDomainString renders a [plan.NumericDomain], or "" when it adds no information.
func numericDomainString(d plan.NumericDomain) string {
	switch d {
	case plan.IntegerOnly:
		return "integer"
	case plan.NonIntegerOnly:
		return "non-integer"
	default:
		return ""
	}
}
