package norm

import "github.com/ogen-go/schemacompiler/internal/ir"

// distributeAll implements common-constraint propagation (design §15.4-15.5,
// §17.3-17.4): T ∩ ExactlyOne(A1..An) = ExactlyOne(T∩A1, ..., T∩An), and the
// same distribution for AnyOf. It applies only when operands contains
// exactly one combinator (ExactlyOne preferred over AnyOf, matching design
// §17.3's example of pushing an AnyOf sibling into oneOf branches) with at
// least one other operand to push in. Multiple same-kind combinator siblings
// are deliberately left factored rather than Cartesian-expanded (design
// §17.6). Each application spends one unit of budget, since it duplicates
// the "other" operands into every branch.
func distributeAll(operands []ir.Expr, st *state) (ir.Expr, bool) {
	eoIdx, eoCount := -1, 0
	anyIdx, anyCount := -1, 0
	for i, o := range operands {
		switch o.(type) {
		case ir.ExactlyOne:
			eoIdx, eoCount = i, eoCount+1
		case ir.AnyOf:
			anyIdx, anyCount = i, anyCount+1
		}
	}

	var idx int
	switch {
	case eoCount == 1:
		idx = eoIdx
	case eoCount == 0 && anyCount == 1:
		idx = anyIdx
	default:
		// Zero combinators: nothing to distribute. Multiple: keep them
		// factored (design §17.6) rather than guessing which to expand.
		return nil, false
	}

	others := make([]ir.Expr, 0, len(operands)-1)
	for i, o := range operands {
		if i != idx {
			others = append(others, o)
		}
	}
	if len(others) == 0 {
		return nil, false // nothing to push in.
	}
	if !st.spend() {
		return nil, false // budget exhausted: stop distributing, stay factored.
	}

	switch c := operands[idx].(type) {
	case ir.ExactlyOne:
		branches := make([]ir.Expr, 0, len(c.Operands))
		for _, b := range c.Operands {
			nb := normalize(ir.All{Operands: append(append([]ir.Expr{}, others...), b)}, st)
			if _, ok := nb.(ir.Never); ok {
				continue // impossible branch elimination (design §15.5).
			}
			branches = append(branches, nb)
		}
		return ir.ExactlyOne{Operands: branches}, true
	case ir.AnyOf:
		branches := make([]ir.Expr, 0, len(c.Operands))
		for _, b := range c.Operands {
			nb := normalize(ir.All{Operands: append(append([]ir.Expr{}, others...), b)}, st)
			if _, ok := nb.(ir.Never); ok {
				continue
			}
			branches = append(branches, nb)
		}
		return ir.AnyOf{Operands: branches}, true
	default:
		return nil, false // unreachable
	}
}
