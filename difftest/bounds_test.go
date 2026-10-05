package difftest

import (
	"context"
	"testing"
	"time"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// deepAnd builds an AND chain of the given depth.
func deepAnd(depth int) expr.Node {
	if depth == 0 {
		return &expr.Lit{Val: true}
	}
	return &expr.And{L: &expr.Lit{Val: true}, R: deepAnd(depth - 1)}
}

// TestDeepInputBounded: a pathologically deep expression must hit the
// step limit on both paths (classified equivalent), not run unbounded.
func TestDeepInputBounded(t *testing.T) {
	cfg := Config{Seed: 1, NumCases: 1, MaxSteps: 100, CaseTimeout: 5 * time.Second}
	c := Case{ID: 0, Expr: deepAnd(100000), Records: []expr.Record{{}}}
	start := time.Now()
	a, b := evalBoth(context.Background(), cfg.withDefaults(), c)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("deep expression evaluation unbounded: %v", elapsed)
	}
	if k := Classify(cfg.withDefaults(), a[0], b[0]); k != DivNone {
		t.Fatalf("both paths must hit the same step limit, got %v", k)
	}
	if errKindOf(a[0].Err) != expr.ErrStepLimit {
		t.Fatalf("want step-limit error, got %v", a[0].Err)
	}
}

// TestReportKeepsOnlyMinimal: after a divergence is confirmed, the
// report retains only the shrunk case, not the original inputs.
func TestReportKeepsOnlyMinimal(t *testing.T) {
	injectBatchFault(t, flipOnA13, func() {
		cfg := Config{Seed: 1, NumCases: 1, MaxRecords: 1}
		c := divergingCase() // 4 records, only one triggers
		r := evalCase(context.Background(), cfg.withDefaults(), c)
		if r.kind != DivValue {
			t.Fatalf("precondition: want DivValue, got %v", r.kind)
		}
		min := Shrink(cfg, c, r.kind)
		if len(min.Records) >= len(c.Records) {
			t.Fatalf("minimal case keeps %d of %d records", len(min.Records), len(c.Records))
		}
	})
}
