package expr

import (
	"context"
	"fmt"
)

// ErrKind classifies evaluation errors so the differential harness can
// compare error categories instead of messages.
type ErrKind int

const (
	ErrNone ErrKind = iota
	ErrUnknownField
	ErrTypeMismatch
	ErrDivByZero
	ErrNullArithmetic
	ErrStepLimit
)

func (k ErrKind) String() string {
	switch k {
	case ErrUnknownField:
		return "unknown-field"
	case ErrTypeMismatch:
		return "type-mismatch"
	case ErrDivByZero:
		return "div-by-zero"
	case ErrNullArithmetic:
		return "null-arithmetic"
	case ErrStepLimit:
		return "step-limit"
	}
	return "none"
}

// Error is a classified evaluation error.
type Error struct {
	Kind ErrKind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errf(kind ErrKind, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Limits bounds resource usage of a single evaluation.
type Limits struct {
	MaxSteps int // max number of node evaluations; 0 means unlimited
}

type interp struct {
	ctx   context.Context
	steps int
	max   int
}

// Eval is the interpreted per-record path. It evaluates n against rec,
// honouring ctx cancellation and the step limit.
//
// Null semantics: nil in arithmetic or ordered comparison is
// ErrNullArithmetic; nil where a bool is required is ErrTypeMismatch;
// equality against nil is defined (nil == nil, nil != non-nil).
func Eval(ctx context.Context, n Node, rec Record, lim Limits) (any, error) {
	it := &interp{ctx: ctx, max: lim.MaxSteps}
	return it.eval(n, rec)
}

func (it *interp) enter() error {
	if err := it.ctx.Err(); err != nil {
		return err
	}
	it.steps++
	if it.max > 0 && it.steps > it.max {
		return errf(ErrStepLimit, "step limit %d exceeded", it.max)
	}
	return nil
}

func (it *interp) eval(n Node, rec Record) (any, error) {
	if err := it.enter(); err != nil {
		return nil, err
	}
	switch t := n.(type) {
	case *Lit:
		return t.Val, nil
	case *Field:
		v, ok := rec[t.Name]
		if !ok {
			return nil, errf(ErrUnknownField, "unknown field %q", t.Name)
		}
		return v, nil
	case *Not:
		v, err := it.eval(t.X, rec)
		if err != nil {
			return nil, err
		}
		b, ok := v.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "NOT expects bool, got %T", v)
		}
		return !b, nil
	case *And:
		l, err := it.eval(t.L, rec)
		if err != nil {
			return nil, err
		}
		lb, ok := l.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "AND expects bool, got %T", l)
		}
		if !lb {
			return false, nil // short-circuit
		}
		r, err := it.eval(t.R, rec)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "AND expects bool, got %T", r)
		}
		return rb, nil
	case *Or:
		l, err := it.eval(t.L, rec)
		if err != nil {
			return nil, err
		}
		lb, ok := l.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "OR expects bool, got %T", l)
		}
		if lb {
			return true, nil // short-circuit
		}
		r, err := it.eval(t.R, rec)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "OR expects bool, got %T", r)
		}
		return rb, nil
	case *Cmp:
		l, err := it.eval(t.L, rec)
		if err != nil {
			return nil, err
		}
		r, err := it.eval(t.R, rec)
		if err != nil {
			return nil, err
		}
		return compare(t.Op, l, r)
	case *Arith:
		l, err := it.eval(t.L, rec)
		if err != nil {
			return nil, err
		}
		r, err := it.eval(t.R, rec)
		if err != nil {
			return nil, err
		}
		return arith(t.Op, l, r)
	case *In:
		x, err := it.eval(t.X, rec)
		if err != nil {
			return nil, err
		}
		for _, e := range t.List {
			ev, err := it.eval(e, rec)
			if err != nil {
				return nil, err
			}
			eq, err := compare(OpEq, x, ev)
			if err != nil {
				return nil, err
			}
			if eq.(bool) {
				return true, nil
			}
		}
		return false, nil
	}
	return nil, errf(ErrTypeMismatch, "unknown node %T", n)
}

func compare(op CmpOp, l, r any) (any, error) {
	// Equality is defined across nulls; ordering is not.
	if l == nil || r == nil {
		switch op {
		case OpEq:
			return l == nil && r == nil, nil
		case OpNe:
			return !(l == nil && r == nil), nil
		default:
			return nil, errf(ErrNullArithmetic, "ordered comparison with null")
		}
	}
	switch lv := l.(type) {
	case float64:
		rv, ok := r.(float64)
		if !ok {
			return nil, errf(ErrTypeMismatch, "cannot compare float64 with %T", r)
		}
		return ordCmp(op, lv, rv), nil
	case string:
		rv, ok := r.(string)
		if !ok {
			return nil, errf(ErrTypeMismatch, "cannot compare string with %T", r)
		}
		return ordCmp(op, lv, rv), nil
	case bool:
		rv, ok := r.(bool)
		if !ok {
			return nil, errf(ErrTypeMismatch, "cannot compare bool with %T", r)
		}
		switch op {
		case OpEq:
			return lv == rv, nil
		case OpNe:
			return lv != rv, nil
		}
		return nil, errf(ErrTypeMismatch, "ordered comparison on bool")
	}
	return nil, errf(ErrTypeMismatch, "uncomparable type %T", l)
}

func ordCmp[T float64 | string](op CmpOp, l, r T) bool {
	switch op {
	case OpEq:
		return l == r
	case OpNe:
		return l != r
	case OpLt:
		return l < r
	case OpLe:
		return l <= r
	case OpGt:
		return l > r
	case OpGe:
		return l >= r
	}
	return false
}

func arith(op ArithOp, l, r any) (any, error) {
	if l == nil || r == nil {
		return nil, errf(ErrNullArithmetic, "arithmetic with null")
	}
	lv, ok := l.(float64)
	if !ok {
		return nil, errf(ErrTypeMismatch, "arithmetic expects float64, got %T", l)
	}
	rv, ok := r.(float64)
	if !ok {
		return nil, errf(ErrTypeMismatch, "arithmetic expects float64, got %T", r)
	}
	switch op {
	case OpAdd:
		return lv + rv, nil
	case OpSub:
		return lv - rv, nil
	case OpMul:
		return lv * rv, nil
	case OpDiv:
		if rv == 0 {
			return nil, errf(ErrDivByZero, "division by zero")
		}
		return lv / rv, nil
	}
	return nil, errf(ErrTypeMismatch, "unknown arith op")
}
