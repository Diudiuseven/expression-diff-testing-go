package difftest

import (
	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// Shrinker reduces a diverging case to a minimal reproducing case.
type Shrinker struct {
	Equiv EquivOptions
	// Check re-evaluates a candidate case and returns its divergence kind.
	// If nil, both engine paths are evaluated directly with default budgets.
	Check func(Case) DivKind
}

// Shrink deterministically minimizes c while preserving the divergence
// classification kind. Reductions are applied in a canonical order and
// iterated to a fixpoint, so the result does not depend on any external
// exploration order. Every accepted candidate is re-verified to still
// trigger the same divergence.
func (s Shrinker) Shrink(c Case, kind DivKind) Case {
	check := s.Check
	if check == nil {
		check = func(cand Case) DivKind {
			a, b := evalBoth(cand)
			k, _ := Classify(a, b, s.Equiv)
			return k
		}
	}
	cur := cloneCase(c)
	for {
		next, ok := s.reduceOnce(cur, kind, check)
		if !ok {
			return cur
		}
		cur = next
	}
}

// reduceOnce tries each canonical reduction and returns the first one that
// preserves the divergence kind.
func (s Shrinker) reduceOnce(c Case, kind DivKind, check func(Case) DivKind) (Case, bool) {
	try := func(cand Case) (Case, bool) {
		if check(cand) == kind {
			return cand, true
		}
		return Case{}, false
	}
	// 1. Drop records (keep at least one).
	if len(c.Records) > 1 {
		for i := range c.Records {
			cand := cloneCase(c)
			cand.Records = append(cand.Records[:i:i], cand.Records[i+1:]...)
			if r, ok := try(cand); ok {
				return r, true
			}
		}
	}
	// 2. Drop record fields.
	for i, rec := range c.Records {
		for name := range rec {
			cand := cloneCase(c)
			delete(cand.Records[i], name)
			if r, ok := try(cand); ok {
				return r, true
			}
		}
	}
	// 3. Null out record field values.
	for i, rec := range c.Records {
		for name, v := range rec {
			if v.Kind == expr.KindNull {
				continue
			}
			cand := cloneCase(c)
			cand.Records[i][name] = expr.Null()
			if r, ok := try(cand); ok {
				return r, true
			}
		}
	}
	// 4. Simplify expression nodes in preorder (canonical).
	var nodes []**expr.Expr
	collectNodes(&c.Expr, &nodes)
	for _, ptr := range nodes {
		n := *ptr
		if n.Op == expr.OpLit {
			continue
		}
		cands := []*expr.Expr{{Op: expr.OpLit, Lit: expr.Null()}}
		if n.Left != nil {
			cands = append(cands, n.Left)
		}
		if n.Right != nil {
			cands = append(cands, n.Right)
		}
		for _, sub := range cands {
			cand := cloneCase(c)
			// Locate the matching node in the clone by index.
			var cn []**expr.Expr
			collectNodes(&cand.Expr, &cn)
			*cn[indexOf(nodes, ptr)] = sub
			if r, ok := try(cand); ok {
				return r, true
			}
		}
	}
	return Case{}, false
}

func indexOf(nodes []**expr.Expr, ptr **expr.Expr) int {
	for i, p := range nodes {
		if p == ptr {
			return i
		}
	}
	return -1
}

// collectNodes gathers pointers to every node slot in preorder.
func collectNodes(slot **expr.Expr, out *[]**expr.Expr) {
	if *slot == nil {
		return
	}
	*out = append(*out, slot)
	collectNodes(&(*slot).Left, out)
	collectNodes(&(*slot).Right, out)
}

func cloneCase(c Case) Case {
	return Case{ID: c.ID, Expr: cloneExpr(c.Expr), Records: cloneRecords(c.Records)}
}

func cloneExpr(e *expr.Expr) *expr.Expr {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Left = cloneExpr(e.Left)
	cp.Right = cloneExpr(e.Right)
	cp.Lit = cloneValue(e.Lit)
	return &cp
}

func cloneRecords(recs []expr.Record) []expr.Record {
	out := make([]expr.Record, len(recs))
	for i, rec := range recs {
		nr := make(expr.Record, len(rec))
		for k, v := range rec {
			nr[k] = cloneValue(v)
		}
		out[i] = nr
	}
	return out
}

func cloneValue(v expr.Value) expr.Value {
	if v.Kind == expr.KindList {
		nl := make([]expr.Value, len(v.L))
		for i, x := range v.L {
			nl[i] = cloneValue(x)
		}
		v.L = nl
	}
	return v
}

// evalBoth evaluates c on the interpreted and the batch path.
func evalBoth(c Case) (Outcome, Outcome) {
	var a Outcome
	for _, rec := range c.Records {
		v, err := expr.Eval(c.Expr, rec, expr.EvalOptions{})
		a.Values = append(a.Values, v)
		a.Errs = append(a.Errs, err)
	}
	br := expr.EvalBatch(c.Expr, c.Records, expr.BatchOptions{})
	return a, Outcome{Values: br.Values, Errs: br.Errs}
}
