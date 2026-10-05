package difftest

import (
	"context"
	"reflect"
	"testing"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// injectBatchFault replaces the batch path with a faulty one for the
// duration of fn.
func injectBatchFault(t *testing.T, faulty func(context.Context, expr.Node, []expr.Record, expr.Limits) []Outcome, fn func()) {
	t.Helper()
	orig := runBatch
	runBatch = faulty
	defer func() { runBatch = orig }()
	fn()
}

// flipOnA13 returns false for any record whose field "a" is 13.0,
// otherwise delegates to the real batch path.
func flipOnA13(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) []Outcome {
	out := safeEvalBatch(ctx, n, recs, lim)
	for i, r := range recs {
		if v, ok := r["a"].(float64); ok && v == 13.0 {
			out[i] = Outcome{Val: false}
		}
	}
	return out
}

func divergingCase() Case {
	return Case{
		ID:   1,
		Expr: &expr.Cmp{Op: expr.OpGt, L: &expr.Field{Name: "a"}, R: &expr.Lit{Val: 10.0}},
		Records: []expr.Record{
			{"a": 1.0, "b": 2.0},
			{"a": 13.0, "b": 99.0, "name": "zzz"}, // triggers the fault
			{"a": 5.0},
			{"flag": true},
		},
	}
}

func TestShrinkProducesMinimalStableCase(t *testing.T) {
	injectBatchFault(t, flipOnA13, func() {
		cfg := Config{Seed: 1}.withDefaults()
		c := divergingCase()
		kind := evalCaseKind(context.Background(), cfg, c)
		if kind != DivValue {
			t.Fatalf("precondition: want DivValue, got %v", kind)
		}
		min := Shrink(cfg, c, kind)

		if got := evalCaseKind(context.Background(), cfg, min); got != kind {
			t.Fatalf("shrunk case no longer triggers %v, got %v", kind, got)
		}
		if len(min.Records) != 1 {
			t.Fatalf("want 1 record after shrink, got %d", len(min.Records))
		}
		rec := min.Records[0]
		if v, ok := rec["a"].(float64); !ok || v != 13.0 {
			t.Fatalf("minimal record must keep a=13.0, got %v", rec)
		}
		if len(rec) != 1 {
			t.Fatalf("irrelevant fields must be removed, got %v", rec)
		}
	})
}

func TestShrinkDeterministic(t *testing.T) {
	injectBatchFault(t, flipOnA13, func() {
		cfg := Config{Seed: 1}.withDefaults()
		m1 := Shrink(cfg, divergingCase(), DivValue)
		m2 := Shrink(cfg, divergingCase(), DivValue)
		if !reflect.DeepEqual(m1, m2) {
			t.Fatalf("shrink not deterministic:\n%+v\n%+v", m1, m2)
		}
	})
}

func TestShrinkExpressionReduced(t *testing.T) {
	injectBatchFault(t, flipOnA13, func() {
		cfg := Config{Seed: 1}.withDefaults()
		c := Case{
			ID: 2,
			Expr: &expr.And{
				L: &expr.Lit{Val: true},
				R: &expr.Or{
					L: &expr.Cmp{Op: expr.OpGt, L: &expr.Field{Name: "a"}, R: &expr.Lit{Val: 10.0}},
					R: &expr.Lit{Val: false},
				},
			},
			Records: []expr.Record{{"a": 13.0}},
		}
		kind := evalCaseKind(context.Background(), cfg, c)
		if kind != DivValue {
			t.Fatalf("precondition: want DivValue, got %v", kind)
		}
		min := Shrink(cfg, c, kind)
		if expr.Size(min.Expr) >= expr.Size(c.Expr) {
			t.Fatalf("expression not shrunk: size %d (was %d)", expr.Size(min.Expr), expr.Size(c.Expr))
		}
		if got := evalCaseKind(context.Background(), cfg, min); got != kind {
			t.Fatalf("shrunk expr lost divergence: %v", got)
		}
	})
}
