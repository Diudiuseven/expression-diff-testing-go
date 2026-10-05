// Package expr implements a record-filtering expression engine with two
// evaluation paths: a per-record interpreter and a batch/short-circuit
// evaluator. Both paths must agree semantically; the difftest package
// verifies that.
package expr

// Kind is the runtime type tag of a Value.
type Kind int

const (
	KindNull Kind = iota
	KindBool
	KindInt
	KindFloat
	KindString
	KindList
)

// Value is a dynamically typed runtime value.
type Value struct {
	Kind Kind
	B    bool
	I    int64
	F    float64
	S    string
	L    []Value
}

// ErrClass classifies evaluation errors so that equivalence checks compare
// error categories rather than message strings.
type ErrClass int

const (
	ErrNone ErrClass = iota
	ErrType          // operand type mismatch
	ErrUnknownField
	ErrDivByZero
	ErrArity
	ErrBudget // evaluation resource budget exhausted
)

// Error is a classified evaluation error.
type Error struct {
	Class ErrClass
	Msg   string
}

func (e *Error) Error() string { return e.Msg }

// Constructors.
func Null() Value           { return Value{Kind: KindNull} }
func Bool(b bool) Value     { return Value{Kind: KindBool, B: b} }
func Int(i int64) Value     { return Value{Kind: KindInt, I: i} }
func Float(f float64) Value { return Value{Kind: KindFloat, F: f} }
func Str(s string) Value    { return Value{Kind: KindString, S: s} }
func List(l []Value) Value  { return Value{Kind: KindList, L: l} }

// ExactEqual reports strict structural equality (no float tolerance).
func (v Value) ExactEqual(o Value) bool {
	if v.Kind != o.Kind {
		return false
	}
	switch v.Kind {
	case KindBool:
		return v.B == o.B
	case KindInt:
		return v.I == o.I
	case KindFloat:
		return v.F == o.F
	case KindString:
		return v.S == o.S
	case KindList:
		if len(v.L) != len(o.L) {
			return false
		}
		for i := range v.L {
			if !v.L[i].ExactEqual(o.L[i]) {
				return false
			}
		}
		return true
	}
	return true // null
}
