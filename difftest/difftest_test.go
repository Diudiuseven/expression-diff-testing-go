package difftest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// --- Seed reproduction ---

func genSignature(cfg GenConfig) string {
	g := NewGenerator(cfg)
	sig := ""
	for {
		c := g.Next()
		if c == nil {
			break
		}
		sig += fmt.Sprintf("%d:%s:", c.ID, c.Expr.String())
		for _, rec := range c.Records {
			sig += fmt.Sprintf("%v;", rec)
		}
		sig += "|"
	}
	return sig
}

func TestSeedReproducesCaseSequence(t *testing.T) {
	cfg := GenConfig{Seed: 42, NumCases: 50, MaxDepth: 4, MaxRecords: 4}
	a := genSignature(cfg)
	b := genSignature(cfg)
	if a != b {
		t.Fatal("same seed produced different case sequences")
	}
	c := genSignature(GenConfig{Seed: 43, NumCases: 50, MaxDepth: 4, MaxRecords: 4})
	if a == c {
		t.Fatal("different seeds produced identical sequences")
	}
}

func TestSeedReproducesConclusions(t *testing.T) {
	cfg := RunConfig{
		Gen:         GenConfig{Seed: 7, NumCases: 60, MaxDepth: 4, MaxRecords: 4},
		CaseTimeout: 5 * time.Second,
		Budget:      30 * time.Second,
	}
	r1 := Run(context.Background(), cfg)
	r2 := Run(context.Background(), cfg)
	if r1.Total != r2.Total || len(r1.Divergences) != len(r2.Divergences) {
		t.Fatalf("runs differ: %+v vs %+v", r1, r2)
	}
	for i := range r1.Divergences {
		if r1.Divergences[i].Kind != r2.Divergences[i].Kind ||
			r1.Divergences[i].Case.ID != r2.Divergences[i].Case.ID {
			t.Fatalf("divergence %d differs between runs", i)
		}
	}
}

// --- Equivalence judgement boundaries ---

func oc(vals ...expr.Value) Outcome {
	return Outcome{Values: vals, Errs: make([]*expr.Error, len(vals))}
}

func TestEquivFloatTolerance(t *testing.T) {
	opts := EquivOptions{FloatTol: 1e-6}
	if !Equivalent(oc(expr.Float(1.0000001)), oc(expr.Float(1.0)), opts) {
		t.Fatal("within tolerance should be equivalent")
	}
	if Equivalent(oc(expr.Float(1.1)), oc(expr.Float(1.0)), opts) {
		t.Fatal("outside tolerance should diverge")
	}
	// int/float numeric equivalence
	if !Equivalent(oc(expr.Int(3)), oc(expr.Float(3.0)), opts) {
		t.Fatal("int 3 and float 3.0 should be equivalent")
	}
}

func TestEquivNullSemantics(t *testing.T) {
	if !Equivalent(oc(expr.Null()), oc(expr.Null()), EquivOptions{}) {
		t.Fatal("null should equal null")
	}
	if Equivalent(oc(expr.Null()), oc(expr.Bool(false)), EquivOptions{}) {
		t.Fatal("null must not equal false")
	}
}

func TestEquivUnorderedLists(t *testing.T) {
	a := expr.List([]expr.Value{expr.Int(1), expr.Str("x"), expr.Float(2.0)})
	b := expr.List([]expr.Value{expr.Float(2.0), expr.Str("x"), expr.Int(1)})
	if !Equivalent(oc(a), oc(b), EquivOptions{}) {
		t.Fatal("list order must not matter")
	}
	c := expr.List([]expr.Value{expr.Int(1), expr.Int(1), expr.Int(2)})
	d := expr.List([]expr.Value{expr.Int(1), expr.Int(2), expr.Int(2)})
	if Equivalent(oc(c), oc(d), EquivOptions{}) {
		t.Fatal("multiset cardinality must matter")
	}
}

func TestEquivErrorClassification(t *testing.T) {
	a := Outcome{Values: []expr.Value{expr.Null()}, Errs: []*expr.Error{{Class: expr.ErrDivByZero, Msg: "x/0"}}}
	b := Outcome{Values: []expr.Value{expr.Null()}, Errs: []*expr.Error{{Class: expr.ErrDivByZero, Msg: "different message"}}}
	if !Equivalent(a, b, EquivOptions{}) {
		t.Fatal("same error class with different messages must be equivalent")
	}
	b.Errs[0].Class = expr.ErrType
	if Equivalent(a, b, EquivOptions{}) {
		t.Fatal("different error classes must diverge")
	}
}

func TestClassifyKinds(t *testing.T) {
	val := oc(expr.Bool(true))
	other := oc(expr.Bool(false))
	errO := Outcome{Values: []expr.Value{expr.Null()}, Errs: []*expr.Error{{Class: expr.ErrType}}}
	errO2 := Outcome{Values: []expr.Value{expr.Null()}, Errs: []*expr.Error{{Class: expr.ErrDivByZero}}}
	if k, _ := Classify(val, other, EquivOptions{}); k != DivValue {
		t.Fatalf("got %v want DivValue", k)
	}
	if k, _ := Classify(val, errO, EquivOptions{}); k != DivValueVsErr {
		t.Fatalf("got %v want DivValueVsErr", k)
	}
	if k, _ := Classify(errO, errO2, EquivOptions{}); k != DivErrClass {
		t.Fatalf("got %v want DivErrClass", k)
	}
	if k, _ := Classify(val, val, EquivOptions{}); k != DivNone {
		t.Fatalf("got %v want DivNone", k)
	}
}

// --- Shrinking ---

// containsDiv is a synthetic divergence predicate for shrinker tests.
func containsDiv(c Case) DivKind {
	var hasDiv func(e *expr.Expr) bool
	hasDiv = func(e *expr.Expr) bool {
		if e == nil {
			return false
		}
		return e.Op == expr.OpDiv || hasDiv(e.Left) || hasDiv(e.Right)
	}
	if hasDiv(c.Expr) && len(c.Records) > 0 {
		return DivValue
	}
	return DivNone
}

func TestShrinkMinimizesAndPreserves(t *testing.T) {
	c := Case{
		Expr: &expr.Expr{Op: expr.OpAnd,
			Left: &expr.Expr{Op: expr.OpGt, Left: &expr.Expr{Op: expr.OpField, Name: "a"}, Right: &expr.Expr{Op: expr.OpLit, Lit: expr.Int(1)}},
			Right: &expr.Expr{Op: expr.OpDiv,
				Left:  &expr.Expr{Op: expr.OpLit, Lit: expr.Int(10)},
				Right: &expr.Expr{Op: expr.OpLit, Lit: expr.Int(2)}}},
		Records: []expr.Record{
			{"a": expr.Int(5), "junk": expr.Str("zz")},
			{"a": expr.Int(1)},
		},
	}
	sh := Shrinker{Check: containsDiv}
	got := sh.Shrink(c, DivValue)
	if containsDiv(got) != DivValue {
		t.Fatal("shrunk case no longer triggers the divergence")
	}
	if got.Expr.Op != expr.OpDiv {
		t.Fatalf("expected minimal expr to be the div node, got %s", got.Expr.String())
	}
	if len(got.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(got.Records))
	}
	// Determinism: shrinking again yields the identical case.
	got2 := sh.Shrink(c, DivValue)
	if got.Expr.String() != got2.Expr.String() {
		t.Fatal("shrink not deterministic")
	}
	// Idempotence: shrinking the result changes nothing.
	got3 := sh.Shrink(got, DivValue)
	if got3.Expr.String() != got.Expr.String() || len(got3.Records) != len(got.Records) {
		t.Fatal("shrink not idempotent")
	}
}

// --- Concurrency consistency ---

func TestConcurrencyDoesNotChangeConclusions(t *testing.T) {
	base := RunConfig{
		Gen:         GenConfig{Seed: 99, NumCases: 120, MaxDepth: 5, MaxRecords: 6},
		CaseTimeout: 5 * time.Second,
		Budget:      60 * time.Second,
	}
	single := base
	single.Workers, single.ShardSize = 1, 1000
	multi := base
	multi.Workers, multi.ShardSize = 8, 3
	r1 := Run(context.Background(), single)
	r2 := Run(context.Background(), multi)
	if r1.Total != r2.Total {
		t.Fatalf("totals differ: %d vs %d", r1.Total, r2.Total)
	}
	if len(r1.Divergences) != len(r2.Divergences) {
		t.Fatalf("divergence counts differ: %d vs %d", len(r1.Divergences), len(r2.Divergences))
	}
	for i := range r1.Divergences {
		d1, d2 := r1.Divergences[i], r2.Divergences[i]
		if d1.Kind != d2.Kind || d1.Case.ID != d2.Case.ID || d1.Case.Expr.String() != d2.Case.Expr.String() {
			t.Fatalf("divergence %d differs across schedules", i)
		}
	}
}

// --- Timeout and cancellation ---

func TestRunSideTimeout(t *testing.T) {
	res := <-runSide(context.Background(), 20*time.Millisecond, func() Outcome {
		time.Sleep(2 * time.Second)
		return oc(expr.Bool(true))
	})
	if !res.timedOut {
		t.Fatal("expected timeout")
	}
}

func TestRunSideCrash(t *testing.T) {
	res := <-runSide(context.Background(), time.Second, func() Outcome {
		panic("boom")
	})
	if !res.crashed {
		t.Fatal("expected crash classification")
	}
}

func TestRunRespectsBudget(t *testing.T) {
	cfg := RunConfig{
		Gen:         GenConfig{Seed: 1, NumCases: 100000, MaxDepth: 6, MaxRecords: 8},
		Workers:     2,
		CaseTimeout: time.Second,
		Budget:      100 * time.Millisecond,
	}
	start := time.Now()
	rep := Run(context.Background(), cfg)
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("run did not stop within budget slack: %v", elapsed)
	}
	if !rep.Stopped {
		t.Fatal("expected Stopped when budget is exhausted")
	}
	if rep.Total >= cfg.Gen.NumCases {
		t.Fatal("expected partial evaluation")
	}
}

func TestRunCancelLeavesNoPartialState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RunConfig{
		Gen:         GenConfig{Seed: 5, NumCases: 5000, MaxDepth: 5},
		CaseTimeout: time.Second,
		Budget:      30 * time.Second,
	}
	done := make(chan Report, 1)
	go func() { done <- Run(ctx, cfg) }()
	cancel()
	rep := <-done
	if rep.Total >= cfg.Gen.NumCases {
		t.Fatal("cancellation had no effect")
	}
	// A subsequent run with the same seed still works and is complete.
	rep2 := Run(context.Background(), cfg)
	if rep2.Total != cfg.Gen.NumCases {
		t.Fatalf("follow-up run incomplete: %d", rep2.Total)
	}
}

// --- End-to-end agreement of both engine paths ---

func TestEndToEndPathsAgree(t *testing.T) {
	cfg := RunConfig{
		Gen:         GenConfig{Seed: 1234, NumCases: 300, MaxDepth: 6, MaxRecords: 8},
		Workers:     4,
		ShardSize:   8,
		CaseTimeout: 5 * time.Second,
		Budget:      60 * time.Second,
	}
	rep := Run(context.Background(), cfg)
	if rep.Stopped {
		t.Fatal("run exhausted budget unexpectedly")
	}
	if rep.Total != 300 {
		t.Fatalf("evaluated %d cases, want 300", rep.Total)
	}
	for _, d := range rep.Divergences {
		t.Errorf("unexpected divergence %v in case %d: %s", d.Kind, d.Case.ID, d.Case.Expr.String())
	}
}
