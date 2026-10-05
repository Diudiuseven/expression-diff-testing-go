package expr

import "fmt"

// Record is one input row.
type Record map[string]Value

// EvalOptions bounds resource usage of a single evaluation.
type EvalOptions struct {
	MaxSteps int // max node visits; 0 means default
}

const defaultMaxSteps = 100000

// Eval interprets e against rec, one record at a time (reference path).
// Errors are classified; panics are not expected but resource bounds are
// enforced through the step budget.
func Eval(e *Expr, rec Record, opts EvalOptions) (Value, *Error) {
	steps := opts.MaxSteps
	if steps <= 0 {
		steps = defaultMaxSteps
	}
	ev := &evaluator{rec: rec, steps: steps}
	v, err := ev.eval(e)
	if err != nil {
		return Null(), err
	}
	return v, nil
}

type evaluator struct {
	rec   Record
	steps int
}

func (ev *evaluator) errf(cls ErrClass, format string, args ...any) *Error {
	return &Error{Class: cls, Msg: fmt.Sprintf(format, args...)}
}

func (ev *evaluator) eval(e *Expr) (Value, *Error) {
	if ev.steps <= 0 {
		return Null(), ev.errf(ErrBudget, "evaluation step budget exhausted")
	}
	ev.steps--
	switch e.Op {
	case OpLit:
		return e.Lit, nil
	case OpField:
		v, ok := ev.rec[e.Name]
		if !ok {
			return Null(), ev.errf(ErrUnknownField, "unknown field %q", e.Name)
		}
		return v, nil
	case OpNot:
		l, err := ev.eval(e.Left)
		if err != nil {
			return Null(), err
		}
		return notValue(l, ev)
	case OpAnd, OpOr:
		return ev.evalLogic(e)
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		return ev.evalCompare(e)
	case OpAdd, OpSub, OpMul, OpDiv:
		return ev.evalArith(e)
	case OpIn:
		return ev.evalIn(e)
	}
	return Null(), ev.errf(ErrType, "unknown op %d", int(e.Op))
}

func notValue(v Value, ev *evaluator) (Value, *Error) {
	if v.Kind == KindNull {
		return Null(), nil
	}
	if v.Kind != KindBool {
		return Null(), ev.errf(ErrType, "not expects bool, got kind %d", v.Kind)
	}
	return Bool(!v.B), nil
}

// truthiness: null propagates, non-bool is a type error.
func (ev *evaluator) asBool(v Value) (b bool, isNull bool, err *Error) {
	switch v.Kind {
	case KindNull:
		return false, true, nil
	case KindBool:
		return v.B, false, nil
	}
	return false, false, ev.errf(ErrType, "expected bool, got kind %d", v.Kind)
}

// SQL three-valued logic for AND/OR.
func (ev *evaluator) evalLogic(e *Expr) (Value, *Error) {
	l, err := ev.eval(e.Left)
	if err != nil {
		return Null(), err
	}
	lb, ln, err := ev.asBool(l)
	if err != nil {
		return Null(), err
	}
	// Short-circuit only when the outcome is decided regardless of the
	// right side: false AND, true OR.
	if e.Op == OpAnd && !ln && !lb {
		return Bool(false), nil
	}
	if e.Op == OpOr && !ln && lb {
		return Bool(true), nil
	}
	r, err := ev.eval(e.Right)
	if err != nil {
		return Null(), err
	}
	rb, rn, err := ev.asBool(r)
	if err != nil {
		return Null(), err
	}
	if e.Op == OpAnd {
		switch {
		case !ln && !rn:
			return Bool(lb && rb), nil
		case (ln && !rn && !rb) || (!ln && !lb && rn):
			return Bool(false), nil
		default:
			return Null(), nil
		}
	}
	// OR
	switch {
	case !ln && !rn:
		return Bool(lb || rb), nil
	case (ln && !rn && rb) || (!ln && lb && rn):
		return Bool(true), nil
	default:
		return Null(), nil
	}
}

// compare returns -1/0/1 with null propagation and numeric promotion.
func compareVals(a, b Value) (cmp int, isNull bool, cls ErrClass) {
	if a.Kind == KindNull || b.Kind == KindNull {
		return 0, true, ErrNone
	}
	numA, aNum := numeric(a)
	numB, bNum := numeric(b)
	if aNum && bNum {
		switch {
		case numA < numB:
			return -1, false, ErrNone
		case numA > numB:
			return 1, false, ErrNone
		}
		return 0, false, ErrNone
	}
	if a.Kind != b.Kind {
		return 0, false, ErrType
	}
	switch a.Kind {
	case KindString:
		switch {
		case a.S < b.S:
			return -1, false, ErrNone
		case a.S > b.S:
			return 1, false, ErrNone
		}
		return 0, false, ErrNone
	case KindBool:
		if a.B == b.B {
			return 0, false, ErrNone
		}
		if !a.B {
			return -1, false, ErrNone
		}
		return 1, false, ErrNone
	}
	return 0, false, ErrType
}

func numeric(v Value) (float64, bool) {
	switch v.Kind {
	case KindInt:
		return float64(v.I), true
	case KindFloat:
		return v.F, true
	}
	return 0, false
}

func (ev *evaluator) evalCompare(e *Expr) (Value, *Error) {
	l, err := ev.eval(e.Left)
	if err != nil {
		return Null(), err
	}
	r, err := ev.eval(e.Right)
	if err != nil {
		return Null(), err
	}
	cmp, isNull, cls := compareVals(l, r)
	if cls != ErrNone {
		return Null(), ev.errf(cls, "cannot compare kinds %d and %d", l.Kind, r.Kind)
	}
	if isNull {
		return Null(), nil
	}
	switch e.Op {
	case OpEq:
		return Bool(cmp == 0), nil
	case OpNe:
		return Bool(cmp != 0), nil
	case OpLt:
		return Bool(cmp < 0), nil
	case OpLe:
		return Bool(cmp <= 0), nil
	case OpGt:
		return Bool(cmp > 0), nil
	case OpGe:
		return Bool(cmp >= 0), nil
	}
	return Null(), ev.errf(ErrType, "bad compare op")
}

func (ev *evaluator) evalArith(e *Expr) (Value, *Error) {
	l, err := ev.eval(e.Left)
	if err != nil {
		return Null(), err
	}
	r, err := ev.eval(e.Right)
	if err != nil {
		return Null(), err
	}
	if l.Kind == KindNull || r.Kind == KindNull {
		return Null(), nil
	}
	lf, lok := numeric(l)
	rf, rok := numeric(r)
	if !lok || !rok {
		return Null(), ev.errf(ErrType, "arithmetic on non-numeric kinds %d, %d", l.Kind, r.Kind)
	}
	if e.Op == OpDiv && rf == 0 {
		return Null(), ev.errf(ErrDivByZero, "division by zero")
	}
	bothInt := l.Kind == KindInt && r.Kind == KindInt
	if bothInt {
		switch e.Op {
		case OpAdd:
			return Int(l.I + r.I), nil
		case OpSub:
			return Int(l.I - r.I), nil
		case OpMul:
			return Int(l.I * r.I), nil
		case OpDiv:
			return Int(l.I / r.I), nil
		}
	}
	switch e.Op {
	case OpAdd:
		return Float(lf + rf), nil
	case OpSub:
		return Float(lf - rf), nil
	case OpMul:
		return Float(lf * rf), nil
	case OpDiv:
		return Float(lf / rf), nil
	}
	return Null(), ev.errf(ErrType, "bad arith op")
}

func (ev *evaluator) evalIn(e *Expr) (Value, *Error) {
	l, err := ev.eval(e.Left)
	if err != nil {
		return Null(), err
	}
	r, err := ev.eval(e.Right)
	if err != nil {
		return Null(), err
	}
	if l.Kind == KindNull {
		return Null(), nil
	}
	if r.Kind != KindList {
		return Null(), ev.errf(ErrType, "in expects list on right, got kind %d", r.Kind)
	}
	sawNull := false
	for _, item := range r.L {
		cmp, isNull, cls := compareVals(l, item)
		if cls != ErrNone {
			return Null(), ev.errf(cls, "in: element not comparable")
		}
		if isNull {
			sawNull = true
			continue
		}
		if cmp == 0 {
			return Bool(true), nil
		}
	}
	if sawNull {
		return Null(), nil
	}
	return Bool(false), nil
}
