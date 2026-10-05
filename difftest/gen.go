package difftest

import (
	"math/rand"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// Case is one generated differential test case.
type Case struct {
	ID      int64
	Expr    expr.Node
	Records []expr.Record
}

// fieldPool is the fixed set of field names a case may reference.
var fieldPool = []string{"a", "b", "c", "flag", "name"}

// Generator produces a deterministic stream of cases from a seed.
// Case i depends only on (Seed, i) and the sizing knobs of the config,
// never on generation order, so sharding and scheduling cannot change
// the produced sequence.
type Generator struct {
	cfg Config
}

// NewGenerator returns a Generator; the same seed and config always
// yield the identical case sequence.
func NewGenerator(cfg Config) *Generator { return &Generator{cfg: cfg.withDefaults()} }

// caseSeed mixes the run seed with the case index.
func (g *Generator) caseSeed(i int) int64 {
	// SplitMix64 over (seed, i); unsigned arithmetic avoids overflow.
	x := uint64(g.cfg.Seed) + uint64(i)*0x9E3779B97F4A7C15
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return int64(x)
}

// Next generates the i-th case deterministically.
func (g *Generator) Next(i int) Case {
	rng := rand.New(rand.NewSource(g.caseSeed(i)))
	eg := &exprGen{rng: rng, maxDepth: g.cfg.MaxDepth}
	n := eg.genBool(0)
	nrec := 1 + rng.Intn(g.cfg.MaxRecords)
	recs := make([]expr.Record, nrec)
	for j := range recs {
		recs[j] = genRecord(rng)
	}
	return Case{ID: int64(i), Expr: n, Records: recs}
}

type exprGen struct {
	rng      *rand.Rand
	maxDepth int
}

// genBool generates an expression, biased towards boolean results so
// cases are usable as record filters.
func (g *exprGen) genBool(depth int) expr.Node {
	if depth >= g.maxDepth {
		return g.genLeaf()
	}
	switch g.rng.Intn(8) {
	case 0:
		return &expr.Not{X: g.genBool(depth + 1)}
	case 1, 2:
		return &expr.And{L: g.genBool(depth + 1), R: g.genBool(depth + 1)}
	case 3, 4:
		return &expr.Or{L: g.genBool(depth + 1), R: g.genBool(depth + 1)}
	case 5, 6:
		return &expr.Cmp{Op: expr.CmpOp(g.rng.Intn(6)), L: g.genValue(depth + 1), R: g.genValue(depth + 1)}
	default:
		n := 1 + g.rng.Intn(3)
		list := make([]expr.Node, n)
		for i := range list {
			list[i] = g.genValue(depth + 1)
		}
		return &expr.In{X: g.genValue(depth + 1), List: list}
	}
}

// genValue generates a value-producing expression.
func (g *exprGen) genValue(depth int) expr.Node {
	if depth >= g.maxDepth {
		return g.genLeaf()
	}
	switch g.rng.Intn(6) {
	case 0, 1:
		return g.genLeaf()
	case 2, 3:
		return &expr.Arith{Op: expr.ArithOp(g.rng.Intn(4)), L: g.genValue(depth + 1), R: g.genValue(depth + 1)}
	default:
		// Occasionally nest a boolean where a value is expected to
		// exercise type-mismatch semantics.
		return g.genBool(depth + 1)
	}
}

func (g *exprGen) genLeaf() expr.Node {
	if g.rng.Intn(2) == 0 {
		return &expr.Field{Name: fieldPool[g.rng.Intn(len(fieldPool))]}
	}
	return &expr.Lit{Val: genScalar(g.rng)}
}

// genScalar produces nil, bool, float64 or string. Zero and near-zero
// floats are included to hit div-by-zero and float-tolerance edges.
func genScalar(rng *rand.Rand) any {
	switch rng.Intn(6) {
	case 0:
		return nil
	case 1:
		return rng.Intn(2) == 0
	case 2:
		return "s" + string(rune('a'+rng.Intn(4)))
	case 3:
		return 0.0
	default:
		return float64(rng.Intn(21) - 10)
	}
}

func genRecord(rng *rand.Rand) expr.Record {
	rec := expr.Record{}
	for _, f := range fieldPool {
		if rng.Intn(5) == 0 { // 20% missing
			continue
		}
		rec[f] = genScalar(rng)
	}
	return rec
}
