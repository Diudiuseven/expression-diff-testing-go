// Package difftest provides differential testing between the interpreted
// and batch evaluation paths of the expr engine: seeded case generation,
// equivalence judgement, divergence classification, shrinking, and a
// concurrent sharded runner.
package difftest

import (
	"math/rand"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// GenConfig controls deterministic case generation.
type GenConfig struct {
	Seed       int64
	NumCases   int
	MaxDepth   int // max expression nesting depth
	MaxRecords int // max records per case
	MaxListLen int
}

func (c GenConfig) withDefaults() GenConfig {
	if c.NumCases <= 0 {
		c.NumCases = 100
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = 5
	}
	if c.MaxRecords <= 0 {
		c.MaxRecords = 8
	}
	if c.MaxListLen <= 0 {
		c.MaxListLen = 4
	}
	return c
}

// Case is one generated test input.
type Case struct {
	ID      int
	Expr    *expr.Expr
	Records []expr.Record
}

// Generator produces a deterministic stream of cases for a fixed seed.
// The same seed and config always yield the identical case sequence and
// therefore identical conclusions.
type Generator struct {
	cfg GenConfig
	rnd *rand.Rand
	n   int
}

// NewGenerator creates a generator bound to cfg.Seed.
func NewGenerator(cfg GenConfig) *Generator {
	cfg = cfg.withDefaults()
	return &Generator{cfg: cfg, rnd: rand.New(rand.NewSource(cfg.Seed))}
}

// Next returns the next case, or nil when the configured count is reached.
func (g *Generator) Next() *Case {
	if g.n >= g.cfg.NumCases {
		return nil
	}
	c := &Case{ID: g.n}
	c.Expr = g.genExpr(g.cfg.MaxDepth)
	nrec := 1 + g.rnd.Intn(g.cfg.MaxRecords)
	c.Records = make([]expr.Record, nrec)
	for i := range c.Records {
		c.Records[i] = g.genRecord()
	}
	g.n++
	return c
}

var fieldPool = []string{"a", "b", "c", "age", "tag", "flag", "name"}

func (g *Generator) genExpr(depth int) *expr.Expr {
	if depth <= 1 {
		return g.genLeaf()
	}
	// Bias towards leaves as depth shrinks.
	if g.rnd.Intn(depth+2) == 0 {
		return g.genLeaf()
	}
	op := expr.Op(int(expr.OpNot) + g.rnd.Intn(int(expr.OpIn)-int(expr.OpNot)+1))
	e := &expr.Expr{Op: op}
	if op == expr.OpNot {
		e.Left = g.genExpr(depth - 1)
		return e
	}
	e.Left = g.genExpr(depth - 1)
	e.Right = g.genExpr(depth - 1)
	return e
}

func (g *Generator) genLeaf() *expr.Expr {
	if g.rnd.Intn(2) == 0 {
		return &expr.Expr{Op: OpField, Name: fieldPool[g.rnd.Intn(len(fieldPool))]}
	}
	return &expr.Expr{Op: OpLit, Lit: g.genValue(false)}
}

// OpField is referenced here to keep the leaf generator readable.
const OpField = expr.OpField
const OpLit = expr.OpLit

func (g *Generator) genRecord() expr.Record {
	rec := expr.Record{}
	// Each field present with probability ~0.7 so unknown-field errors occur.
	for _, f := range fieldPool {
		if g.rnd.Intn(10) < 7 {
			rec[f] = g.genValue(true)
		}
	}
	return rec
}

func (g *Generator) genValue(allowList bool) expr.Value {
	kinds := 6
	if allowList {
		kinds = 7
	}
	switch g.rnd.Intn(kinds) {
	case 0:
		return expr.Null()
	case 1:
		return expr.Bool(g.rnd.Intn(2) == 0)
	case 2:
		return expr.Int(int64(g.rnd.Intn(21) - 10))
	case 3:
		// Include values near zero to exercise division and tolerances.
		return expr.Float(float64(g.rnd.Intn(41)-20) / 4.0)
	case 4:
		return expr.Str(string(rune('a' + g.rnd.Intn(4))))
	default:
		if allowList {
			n := g.rnd.Intn(g.cfg.MaxListLen + 1)
			l := make([]expr.Value, n)
			for i := range l {
				l[i] = g.genValue(false)
			}
			return expr.List(l)
		}
		return expr.Int(int64(g.rnd.Intn(5)))
	}
}
