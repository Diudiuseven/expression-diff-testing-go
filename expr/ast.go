package expr

import (
	"fmt"
	"strconv"
	"strings"
)

// Op identifies a node operation.
type Op int

const (
	OpLit   Op = iota // literal in Lit
	OpField           // record field reference in Name
	OpNot
	OpAnd
	OpOr
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpIn // Left in Right(list)
)

var opNames = map[Op]string{
	OpLit: "lit", OpField: "field", OpNot: "not", OpAnd: "and", OpOr: "or",
	OpEq: "eq", OpNe: "ne", OpLt: "lt", OpLe: "le", OpGt: "gt", OpGe: "ge",
	OpAdd: "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpIn: "in",
}

// Depth returns the nesting depth of the tree.
func (e *Expr) Depth() int {
	if e == nil {
		return 0
	}
	d := 1
	if e.Left != nil {
		if l := e.Left.Depth() + 1; l > d {
			d = l
		}
	}
	if e.Right != nil {
		if r := e.Right.Depth() + 1; r > d {
			d = r
		}
	}
	return d
}

// Size returns the number of nodes.
func (e *Expr) Size() int {
	if e == nil {
		return 0
	}
	return 1 + e.Left.Size() + e.Right.Size()
}

// String renders the expression in a canonical s-expression form.
func (e *Expr) String() string {
	if e == nil {
		return "<nil>"
	}
	var sb strings.Builder
	e.writeTo(&sb)
	return sb.String()
}

func (e *Expr) writeTo(sb *strings.Builder) {
	switch e.Op {
	case OpLit:
		sb.WriteString(formatValue(e.Lit))
	case OpField:
		sb.WriteString("$" + e.Name)
	case OpNot:
		fmt.Fprintf(sb, "(not %s)", e.Left.String())
	default:
		fmt.Fprintf(sb, "(%s %s %s)", opNames[e.Op], e.Left.String(), e.Right.String())
	}
}

func formatValue(v Value) string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindBool:
		return strconv.FormatBool(v.B)
	case KindInt:
		return strconv.FormatInt(v.I, 10)
	case KindFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case KindString:
		return strconv.Quote(v.S)
	case KindList:
		parts := make([]string, len(v.L))
		for i, x := range v.L {
			parts[i] = formatValue(x)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return "?"
}

// Expr is an expression AST. Unary ops use Left; binary ops use Left+Right.
type Expr struct {
	Op    Op
	Lit   Value
	Name  string
	Left  *Expr
	Right *Expr
}
