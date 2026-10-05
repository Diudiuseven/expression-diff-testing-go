package expr

import "fmt"

// BatchOptions controls the batch/short-circuit evaluation path.
type BatchOptions struct {
	BatchSize int // rows per vector; 0 means default
	MaxSteps  int // total node-visit budget across the batch; 0 means default
}

const defaultBatchSize = 1024

// BatchResult holds the per-record outcome of the batch path.
type BatchResult struct {
	Values []Value
	Errs   []*Error // per-row classified error, nil if row succeeded
}

// EvalBatch evaluates e over many records at once. It is the optimized
// path: rows are processed in columnar chunks and logical operators prune
// rows whose outcome is already decided (short-circuit), so the right
// operand is only evaluated for the surviving row subset.
func EvalBatch(e *Expr, recs []Record, opts BatchOptions) *BatchResult {
	bs := opts.BatchSize
	if bs <= 0 {
		bs = defaultBatchSize
	}
	steps := opts.MaxSteps
	if steps <= 0 {
		steps = defaultMaxSteps * len(recs)
	}
	res := &BatchResult{
		Values: make([]Value, len(recs)),
		Errs:   make([]*Error, len(recs)),
	}
	be := &batchEval{steps: steps}
	for start := 0; start < len(recs); start += bs {
		end := start + bs
		if end > len(recs) {
			end = len(recs)
		}
		col := be.eval(e, recs[start:end])
		for i := 0; i < end-start; i++ {
			res.Values[start+i] = col.vals[i]
			res.Errs[start+i] = col.errs[i]
		}
	}
	return res
}

type column struct {
	vals []Value
	errs []*Error
}

type batchEval struct {
	steps int
}

func (be *batchEval) errf(cls ErrClass, format string, args ...any) *Error {
	return &Error{Class: cls, Msg: fmt.Sprintf(format, args...)}
}

// eval computes e for every row of recs. Rows that already carry an error
// keep it; remaining rows are evaluated.
func (be *batchEval) eval(e *Expr, recs []Record) column {
	n := len(recs)
	col := column{vals: make([]Value, n), errs: make([]*Error, n)}
	be.steps -= n
	if be.steps < 0 {
		for i := range col.errs {
			col.errs[i] = be.errf(ErrBudget, "evaluation step budget exhausted")
		}
		return col
	}
	switch e.Op {
	case OpLit:
		for i := range col.vals {
			col.vals[i] = e.Lit
		}
		return col
	case OpField:
		for i, rec := range recs {
			v, ok := rec[e.Name]
			if !ok {
				col.errs[i] = be.errf(ErrUnknownField, "unknown field %q", e.Name)
				continue
			}
			col.vals[i] = v
		}
		return col
	case OpNot:
		l := be.eval(e.Left, recs)
		for i := range col.vals {
			if l.errs[i] != nil {
				col.errs[i] = l.errs[i]
				continue
			}
			v := l.vals[i]
			switch v.Kind {
			case KindNull:
				col.vals[i] = Null()
			case KindBool:
				col.vals[i] = Bool(!v.B)
			default:
				col.errs[i] = be.errf(ErrType, "not expects bool, got kind %d", v.Kind)
			}
		}
		return col
	case OpAnd, OpOr:
		return be.evalLogic(e, recs)
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		l := be.eval(e.Left, recs)
		r := be.eval(e.Right, recs)
		return be.zipCompare(e.Op, l, r)
	case OpAdd, OpSub, OpMul, OpDiv:
		l := be.eval(e.Left, recs)
		r := be.eval(e.Right, recs)
		return be.zipArith(e.Op, l, r)
	case OpIn:
		l := be.eval(e.Left, recs)
		r := be.eval(e.Right, recs)
		return be.zipIn(l, r)
	}
	for i := range col.errs {
		col.errs[i] = be.errf(ErrType, "unknown op %d", int(e.Op))
	}
	return col
}

// evalLogic implements SQL three-valued logic with row pruning: rows whose
// outcome is decided by the left operand alone never evaluate the right
// operand, and their right-side errors are never observed (matching the
// interpreter's short-circuit).
func (be *batchEval) evalLogic(e *Expr, recs []Record) column {
	n := len(recs)
	out := column{vals: make([]Value, n), errs: make([]*Error, n)}
	l := be.eval(e.Left, recs)

	// Rows still needing the right operand.
	var idx []int
	var sub []Record
	for i := 0; i < n; i++ {
		if l.errs[i] != nil {
			out.errs[i] = l.errs[i]
			continue
		}
		v := l.vals[i]
		switch v.Kind {
		case KindBool:
			if e.Op == OpAnd && !v.B {
				out.vals[i] = Bool(false) // false AND _ = false
				continue
			}
			if e.Op == OpOr && v.B {
				out.vals[i] = Bool(true) // true OR _ = true
				continue
			}
		case KindNull:
			// undecided, need right side
		default:
			out.errs[i] = be.errf(ErrType, "expected bool, got kind %d", v.Kind)
			continue
		}
		idx = append(idx, i)
		sub = append(sub, recs[i])
	}
	if len(idx) == 0 {
		return out
	}
	r := be.eval(e.Right, sub)
	for j, i := range idx {
		if r.errs[j] != nil {
			out.errs[i] = r.errs[j]
			continue
		}
		lv, rv := l.vals[i], r.vals[j]
		rb, rn, rerr := boolOf(rv)
		if rerr != nil {
			out.errs[i] = be.errf(ErrType, "expected bool, got kind %d", rv.Kind)
			continue
		}
		lb, ln, _ := boolOf(lv) // lv already validated as bool/null
		if e.Op == OpAnd {
			switch {
			case !ln && !rn:
				out.vals[i] = Bool(lb && rb)
			case (ln && !rn && !rb) || (!ln && !lb && rn):
				out.vals[i] = Bool(false)
			default:
				out.vals[i] = Null()
			}
		} else {
			switch {
			case !ln && !rn:
				out.vals[i] = Bool(lb || rb)
			case (ln && !rn && rb) || (!ln && lb && rn):
				out.vals[i] = Bool(true)
			default:
				out.vals[i] = Null()
			}
		}
	}
	return out
}

func boolOf(v Value) (b bool, isNull bool, err *Error) {
	switch v.Kind {
	case KindNull:
		return false, true, nil
	case KindBool:
		return v.B, false, nil
	}
	return false, false, &Error{Class: ErrType}
}

func (be *batchEval) zipCompare(op Op, l, r column) column {
	n := len(l.vals)
	out := column{vals: make([]Value, n), errs: make([]*Error, n)}
	for i := 0; i < n; i++ {
		if l.errs[i] != nil {
			out.errs[i] = l.errs[i]
			continue
		}
		if r.errs[i] != nil {
			out.errs[i] = r.errs[i]
			continue
		}
		cmp, isNull, cls := compareVals(l.vals[i], r.vals[i])
		if cls != ErrNone {
			out.errs[i] = be.errf(cls, "cannot compare kinds %d and %d", l.vals[i].Kind, r.vals[i].Kind)
			continue
		}
		if isNull {
			out.vals[i] = Null()
			continue
		}
		switch op {
		case OpEq:
			out.vals[i] = Bool(cmp == 0)
		case OpNe:
			out.vals[i] = Bool(cmp != 0)
		case OpLt:
			out.vals[i] = Bool(cmp < 0)
		case OpLe:
			out.vals[i] = Bool(cmp <= 0)
		case OpGt:
			out.vals[i] = Bool(cmp > 0)
		case OpGe:
			out.vals[i] = Bool(cmp >= 0)
		}
	}
	return out
}

func (be *batchEval) zipArith(op Op, l, r column) column {
	n := len(l.vals)
	out := column{vals: make([]Value, n), errs: make([]*Error, n)}
	for i := 0; i < n; i++ {
		if l.errs[i] != nil {
			out.errs[i] = l.errs[i]
			continue
		}
		if r.errs[i] != nil {
			out.errs[i] = r.errs[i]
			continue
		}
		lv, rv := l.vals[i], r.vals[i]
		if lv.Kind == KindNull || rv.Kind == KindNull {
			out.vals[i] = Null()
			continue
		}
		lf, lok := numeric(lv)
		rf, rok := numeric(rv)
		if !lok || !rok {
			out.errs[i] = be.errf(ErrType, "arithmetic on non-numeric kinds %d, %d", lv.Kind, rv.Kind)
			continue
		}
		if op == OpDiv && rf == 0 {
			out.errs[i] = be.errf(ErrDivByZero, "division by zero")
			continue
		}
		if lv.Kind == KindInt && rv.Kind == KindInt {
			switch op {
			case OpAdd:
				out.vals[i] = Int(lv.I + rv.I)
			case OpSub:
				out.vals[i] = Int(lv.I - rv.I)
			case OpMul:
				out.vals[i] = Int(lv.I * rv.I)
			case OpDiv:
				out.vals[i] = Int(lv.I / rv.I)
			}
			continue
		}
		switch op {
		case OpAdd:
			out.vals[i] = Float(lf + rf)
		case OpSub:
			out.vals[i] = Float(lf - rf)
		case OpMul:
			out.vals[i] = Float(lf * rf)
		case OpDiv:
			out.vals[i] = Float(lf / rf)
		}
	}
	return out
}

func (be *batchEval) zipIn(l, r column) column {
	n := len(l.vals)
	out := column{vals: make([]Value, n), errs: make([]*Error, n)}
	for i := 0; i < n; i++ {
		if l.errs[i] != nil {
			out.errs[i] = l.errs[i]
			continue
		}
		if r.errs[i] != nil {
			out.errs[i] = r.errs[i]
			continue
		}
		lv, rv := l.vals[i], r.vals[i]
		if lv.Kind == KindNull {
			out.vals[i] = Null()
			continue
		}
		if rv.Kind != KindList {
			out.errs[i] = be.errf(ErrType, "in expects list on right, got kind %d", rv.Kind)
			continue
		}
		sawNull := false
		matched := false
		for _, item := range rv.L {
			cmp, isNull, cls := compareVals(lv, item)
			if cls != ErrNone {
				out.errs[i] = be.errf(cls, "in: element not comparable")
				break
			}
			if isNull {
				sawNull = true
				continue
			}
			if cmp == 0 {
				matched = true
				break
			}
		}
		if out.errs[i] != nil {
			continue
		}
		switch {
		case matched:
			out.vals[i] = Bool(true)
		case sawNull:
			out.vals[i] = Null()
		default:
			out.vals[i] = Bool(false)
		}
	}
	return out
}
