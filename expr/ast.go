// Package expr defines the record-filter expression AST shared by the
// interpreted (per-record) path and the batch/short-circuit optimized path.
package expr

// CmpOp is a comparison operator.
type CmpOp int

const (
	OpEq CmpOp = iota
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
)

// ArithOp is an arithmetic operator.
type ArithOp int

const (
	OpAdd ArithOp = iota
	OpSub
	OpMul
	OpDiv
)

// Node is an expression AST node.
type Node interface{ isNode() }

// Lit is a literal: nil, bool, float64 or string.
type Lit struct{ Val any }

// Field references a record field by name.
type Field struct{ Name string }

// Not negates a boolean operand.
type Not struct{ X Node }

// And is logical conjunction (short-circuit).
type And struct{ L, R Node }

// Or is logical disjunction (short-circuit).
type Or struct{ L, R Node }

// Cmp compares two operands.
type Cmp struct {
	Op   CmpOp
	L, R Node
}

// Arith applies an arithmetic operator.
type Arith struct {
	Op   ArithOp
	L, R Node
}

// In tests membership of X in List. The result is a set-valued
// (unordered) boolean evaluation; List order is not semantic.
type In struct {
	X    Node
	List []Node
}

func (*Lit) isNode()   {}
func (*Field) isNode() {}
func (*Not) isNode()   {}
func (*And) isNode()   {}
func (*Or) isNode()    {}
func (*Cmp) isNode()   {}
func (*Arith) isNode() {}
func (*In) isNode()    {}

// Record is one input row.
type Record map[string]any

// Depth returns the nesting depth of n.
func Depth(n Node) int {
	switch t := n.(type) {
	case *Lit, *Field:
		return 1
	case *Not:
		return 1 + Depth(t.X)
	case *And:
		return 1 + max(Depth(t.L), Depth(t.R))
	case *Or:
		return 1 + max(Depth(t.L), Depth(t.R))
	case *Cmp:
		return 1 + max(Depth(t.L), Depth(t.R))
	case *Arith:
		return 1 + max(Depth(t.L), Depth(t.R))
	case *In:
		d := Depth(t.X)
		for _, e := range t.List {
			if de := Depth(e); de > d {
				d = de
			}
		}
		return 1 + d
	}
	return 0
}

// Size returns the number of nodes in n.
func Size(n Node) int {
	switch t := n.(type) {
	case *Lit, *Field:
		return 1
	case *Not:
		return 1 + Size(t.X)
	case *And:
		return 1 + Size(t.L) + Size(t.R)
	case *Or:
		return 1 + Size(t.L) + Size(t.R)
	case *Cmp:
		return 1 + Size(t.L) + Size(t.R)
	case *Arith:
		return 1 + Size(t.L) + Size(t.R)
	case *In:
		s := 1 + Size(t.X)
		for _, e := range t.List {
			s += Size(e)
		}
		return s
	}
	return 0
}
