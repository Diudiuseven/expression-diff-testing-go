package difftest

import (
	"context"
	"sort"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// Shrink reduces a diverging case to a minimal case that still triggers
// the same divergence kind. The exploration order is fixed (records by
// index, fields by name, expression nodes breadth-first), so the result
// is a deterministic function of the input case and independent of any
// external ordering.
func Shrink(cfg Config, c Case, kind DivKind) Case {
	return ShrinkCtx(context.Background(), cfg, c, kind)
}

// ShrinkCtx is Shrink bound to a caller context: when ctx is cancelled
// the search stops and the smallest case found so far is returned, so a
// cancelled run can always shut down promptly.
func ShrinkCtx(ctx context.Context, cfg Config, c Case, kind DivKind) Case {
	cfg = cfg.withDefaults()
	triggers := func(cand Case) bool {
		if ctx.Err() != nil || len(cand.Records) == 0 {
			return false
		}
		return evalCaseKind(ctx, cfg, cand) == kind
	}

	c = Case{ID: c.ID, Expr: cloneNode(c.Expr), Records: cloneRecords(c.Records)}
	for changed := true; changed; {
		changed = false
		if shrinkRecords(c.Records, triggers, &c) {
			changed = true
		}
		if shrinkFields(&c, triggers) {
			changed = true
		}
		if e, ok := shrinkExpr(c, triggers); ok {
			c.Expr = e
			changed = true
		}
	}
	return c
}

// shrinkRecords applies delta-debugging style chunk removal, then
// one-by-one removal, keeping the first reduction that preserves the
// divergence.
func shrinkRecords(recs []expr.Record, triggers func(Case) bool, c *Case) bool {
	progress := false
	for len(c.Records) > 1 {
		n := 2
		reduced := false
		for n <= len(c.Records) {
			chunk := (len(c.Records) + n - 1) / n
			done := true
			for i := 0; i < len(c.Records); i += chunk {
				j := i + chunk
				if j > len(c.Records) {
					j = len(c.Records)
				}
				cand := make([]expr.Record, 0, len(c.Records)-(j-i))
				cand = append(cand, c.Records[:i]...)
				cand = append(cand, c.Records[j:]...)
				if len(cand) == 0 {
					continue
				}
				if triggers(Case{Expr: c.Expr, Records: cand}) {
					c.Records = cand
					progress, reduced = true, true
					done = false
					break
				}
			}
			if done || n >= len(c.Records) {
				break
			}
			if !reduced {
				n *= 2
			}
		}
		if !reduced {
			break
		}
	}
	return progress
}

// shrinkFields tries deleting each field of each record, in
// (record index, field name) order.
func shrinkFields(c *Case, triggers func(Case) bool) bool {
	progress := false
	for i := 0; i < len(c.Records); i++ {
		names := make([]string, 0, len(c.Records[i]))
		for name := range c.Records[i] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			old, ok := c.Records[i][name]
			if !ok {
				continue
			}
			delete(c.Records[i], name)
			if triggers(*c) {
				progress = true
			} else {
				c.Records[i][name] = old // restore
			}
		}
	}
	return progress
}

// shrinkExpr greedily replaces the expression by a sub-expression that
// still triggers the divergence, iterating to a fixpoint. Candidates
// are enumerated breadth-first for determinism. The second return value
// reports whether any simplification was applied.
func shrinkExpr(c Case, triggers func(Case) bool) (expr.Node, bool) {
	cur := c.Expr
	changed := false
	for {
		var next expr.Node
		for _, cand := range subExprs(cur) {
			if triggers(Case{Expr: cand, Records: c.Records}) {
				next = cand
				break
			}
		}
		if next == nil {
			return cur, changed
		}
		cur = next
		changed = true
	}
}

// subExprs lists the immediate sub-expressions of n in a fixed order.
func subExprs(n expr.Node) []expr.Node {
	switch t := n.(type) {
	case *expr.Not:
		return []expr.Node{t.X}
	case *expr.And:
		return []expr.Node{t.L, t.R}
	case *expr.Or:
		return []expr.Node{t.L, t.R}
	case *expr.Cmp:
		return []expr.Node{t.L, t.R}
	case *expr.Arith:
		return []expr.Node{t.L, t.R}
	case *expr.In:
		out := make([]expr.Node, 0, len(t.List)+1)
		out = append(out, t.X)
		out = append(out, t.List...)
		return out
	}
	return nil
}

func cloneNode(n expr.Node) expr.Node {
	switch t := n.(type) {
	case *expr.Lit:
		return &expr.Lit{Val: t.Val}
	case *expr.Field:
		return &expr.Field{Name: t.Name}
	case *expr.Not:
		return &expr.Not{X: cloneNode(t.X)}
	case *expr.And:
		return &expr.And{L: cloneNode(t.L), R: cloneNode(t.R)}
	case *expr.Or:
		return &expr.Or{L: cloneNode(t.L), R: cloneNode(t.R)}
	case *expr.Cmp:
		return &expr.Cmp{Op: t.Op, L: cloneNode(t.L), R: cloneNode(t.R)}
	case *expr.Arith:
		return &expr.Arith{Op: t.Op, L: cloneNode(t.L), R: cloneNode(t.R)}
	case *expr.In:
		list := make([]expr.Node, len(t.List))
		for i, e := range t.List {
			list[i] = cloneNode(e)
		}
		return &expr.In{X: cloneNode(t.X), List: list}
	}
	return n
}

func cloneRecords(recs []expr.Record) []expr.Record {
	out := make([]expr.Record, len(recs))
	for i, r := range recs {
		nr := make(expr.Record, len(r))
		for k, v := range r {
			nr[k] = v
		}
		out[i] = nr
	}
	return out
}
