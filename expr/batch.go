package expr

import "context"

// BatchResult holds the per-record outcome of the batch path.
type BatchResult struct {
	Vals []any
	Errs []error
}

// EvalBatch is the optimized path: it evaluates n over many records at
// once with vectorized short-circuiting (right operands are evaluated
// only for records whose outcome is not yet decided) and constant
// folding. It is semantically equivalent to calling Eval per record,
// including per-record error and step-limit behaviour.
func EvalBatch(ctx context.Context, n Node, recs []Record, lim Limits) (*BatchResult, error) {
	// Constant folding changes the number of evaluation steps, so it is
	// disabled when a step limit is in force to keep per-record step
	// accounting identical to the interpreted path.
	if lim.MaxSteps == 0 {
		n = fold(ctx, n)
	}
	b := &batch{
		ctx:   ctx,
		recs:  recs,
		steps: make([]int, len(recs)),
		max:   lim.MaxSteps,
	}
	res := &BatchResult{Vals: make([]any, len(recs)), Errs: make([]error, len(recs))}
	active := make([]int, len(recs))
	for i := range recs {
		active[i] = i
	}
	if err := b.evalNode(n, active, res); err != nil {
		return nil, err // only context cancellation aborts the whole batch
	}
	return res, nil
}

type batch struct {
	ctx   context.Context
	recs  []Record
	steps []int
	max   int
}

// enter accounts one step for record i; returns false if i is done
// (step limit hit, recorded as an error) and aborts on ctx cancel.
func (b *batch) enter(i int, res *BatchResult) (bool, error) {
	if err := b.ctx.Err(); err != nil {
		return false, err
	}
	b.steps[i]++
	if b.max > 0 && b.steps[i] > b.max {
		res.Errs[i] = errf(ErrStepLimit, "step limit %d exceeded", b.max)
		return false, nil
	}
	return true, nil
}

// evalNode evaluates n for all active record indices, writing results
// into res. A non-nil error return means the whole run was cancelled.
func (b *batch) evalNode(n Node, active []int, res *BatchResult) error {
	if len(active) == 0 {
		return nil
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	switch t := n.(type) {
	case *Lit:
		for _, i := range active {
			ok, err := b.enter(i, res)
			if err != nil {
				return err
			}
			if ok {
				res.Vals[i] = t.Val
			}
		}
	case *Field:
		for _, i := range active {
			ok, err := b.enter(i, res)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			v, found := b.recs[i][t.Name]
			if !found {
				res.Errs[i] = errf(ErrUnknownField, "unknown field %q", t.Name)
			} else {
				res.Vals[i] = v
			}
		}
	case *Not:
		if err := b.evalNode(t.X, active, res); err != nil {
			return err
		}
		for _, i := range active {
			if res.Errs[i] != nil {
				continue
			}
			ok, err := b.enter(i, res)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			v, isBool := res.Vals[i].(bool)
			if !isBool {
				res.Errs[i] = errf(ErrTypeMismatch, "NOT expects bool, got %T", res.Vals[i])
				continue
			}
			res.Vals[i] = !v
		}
	case *And:
		return b.evalAndOr(t.L, t.R, true, active, res)
	case *Or:
		return b.evalAndOr(t.L, t.R, false, active, res)
	case *Cmp:
		lv := make([]any, len(b.recs))
		if err := b.evalNodeSave(t.L, active, res, lv); err != nil {
			return err
		}
		rest := b.alive(active, res)
		if err := b.evalNode(t.R, rest, res); err != nil {
			return err
		}
		for _, i := range b.alive(active, res) {
			ok, err := b.enter(i, res)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			v, err := compare(t.Op, lv[i], res.Vals[i])
			if err != nil {
				res.Errs[i] = err
			} else {
				res.Vals[i] = v
			}
		}
	case *Arith:
		lv := make([]any, len(b.recs))
		if err := b.evalNodeSave(t.L, active, res, lv); err != nil {
			return err
		}
		rest := b.alive(active, res)
		rv := make([]any, len(b.recs))
		if err := b.evalNodeSave(t.R, rest, res, rv); err != nil {
			return err
		}
		for _, i := range b.alive(active, res) {
			ok, err := b.enter(i, res)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			v, err := arith(t.Op, lv[i], rv[i])
			if err != nil {
				res.Errs[i] = err
			} else {
				res.Vals[i] = v
			}
		}
	case *In:
		xv := make([]any, len(b.recs))
		if err := b.evalNodeSave(t.X, active, res, xv); err != nil {
			return err
		}
		remaining := b.alive(active, res)
		matched := make([]bool, len(b.recs))
		for _, e := range t.List {
			if len(remaining) == 0 {
				break
			}
			ev := make([]any, len(b.recs))
			if err := b.evalNodeSave(e, remaining, res, ev); err != nil {
				return err
			}
			var next []int
			for _, i := range remaining {
				if res.Errs[i] != nil {
					continue
				}
				ok, err := b.enter(i, res)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				eq, err := compare(OpEq, xv[i], ev[i])
				if err != nil {
					res.Errs[i] = err
					continue
				}
				if eq.(bool) {
					matched[i] = true
				} else {
					next = append(next, i)
				}
			}
			remaining = next
		}
		for _, i := range b.alive(active, res) {
			res.Vals[i] = matched[i]
		}
	}
	return nil
}

// evalAndOr evaluates L then R only for records not decided by L.
func (b *batch) evalAndOr(l, r Node, isAnd bool, active []int, res *BatchResult) error {
	if err := b.evalNode(l, active, res); err != nil {
		return err
	}
	var rest []int
	for _, i := range active {
		if res.Errs[i] != nil {
			continue
		}
		ok, err := b.enter(i, res)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		v, isBool := res.Vals[i].(bool)
		if !isBool {
			res.Errs[i] = errf(ErrTypeMismatch, "AND/OR expects bool, got %T", res.Vals[i])
			continue
		}
		if isAnd && !v {
			res.Vals[i] = false // short-circuit
		} else if !isAnd && v {
			res.Vals[i] = true // short-circuit
		} else {
			rest = append(rest, i)
		}
	}
	if err := b.evalNode(r, rest, res); err != nil {
		return err
	}
	for _, i := range rest {
		if res.Errs[i] != nil {
			continue
		}
		v, isBool := res.Vals[i].(bool)
		if !isBool {
			res.Errs[i] = errf(ErrTypeMismatch, "AND/OR expects bool, got %T", res.Vals[i])
			continue
		}
		res.Vals[i] = v
	}
	return nil
}

// alive returns the indices in active with no recorded error.
func (b *batch) alive(active []int, res *BatchResult) []int {
	out := make([]int, 0, len(active))
	for _, i := range active {
		if res.Errs[i] == nil {
			out = append(out, i)
		}
	}
	return out
}

// evalNodeSave evaluates n like evalNode but copies surviving values
// into save so a sibling subtree can overwrite res.Vals.
func (b *batch) evalNodeSave(n Node, active []int, res *BatchResult, save []any) error {
	if err := b.evalNode(n, active, res); err != nil {
		return err
	}
	for _, i := range active {
		if res.Errs[i] == nil {
			save[i] = res.Vals[i]
		}
	}
	return nil
}

// fold returns a copy of n in which constant subtrees (no Field
// references) that evaluate cleanly are replaced by a single Lit.
// Error-producing constant subtrees are left untouched so per-record
// error semantics are preserved. The input tree is never mutated, so
// concurrent evaluators can share it safely.
func fold(ctx context.Context, n Node) Node {
	var rebuilt Node
	switch t := n.(type) {
	case *Lit, *Field:
		return n
	case *Not:
		rebuilt = &Not{X: fold(ctx, t.X)}
	case *And:
		rebuilt = &And{L: fold(ctx, t.L), R: fold(ctx, t.R)}
	case *Or:
		rebuilt = &Or{L: fold(ctx, t.L), R: fold(ctx, t.R)}
	case *Cmp:
		rebuilt = &Cmp{Op: t.Op, L: fold(ctx, t.L), R: fold(ctx, t.R)}
	case *Arith:
		rebuilt = &Arith{Op: t.Op, L: fold(ctx, t.L), R: fold(ctx, t.R)}
	case *In:
		list := make([]Node, len(t.List))
		for i, e := range t.List {
			list[i] = fold(ctx, e)
		}
		rebuilt = &In{X: fold(ctx, t.X), List: list}
	default:
		return n
	}
	if hasField(rebuilt) {
		return rebuilt
	}
	v, err := Eval(ctx, rebuilt, Record{}, Limits{})
	if err != nil {
		return rebuilt
	}
	return &Lit{Val: v}
}

func hasField(n Node) bool {
	switch t := n.(type) {
	case *Field:
		return true
	case *Not:
		return hasField(t.X)
	case *And:
		return hasField(t.L) || hasField(t.R)
	case *Or:
		return hasField(t.L) || hasField(t.R)
	case *Cmp:
		return hasField(t.L) || hasField(t.R)
	case *Arith:
		return hasField(t.L) || hasField(t.R)
	case *In:
		if hasField(t.X) {
			return true
		}
		for _, e := range t.List {
			if hasField(e) {
				return true
			}
		}
	}
	return false
}
