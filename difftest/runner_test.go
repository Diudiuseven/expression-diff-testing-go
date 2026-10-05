package difftest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// TestRunTwoPathsAgree is the core differential test: the optimized
// batch path must agree with the interpreted path on generated cases.
func TestRunTwoPathsAgree(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 7, 12345} {
		rep := Run(context.Background(), Config{Seed: seed, NumCases: 200})
		if rep.Total != 200 {
			t.Fatalf("seed %d: processed %d of 200", seed, rep.Total)
		}
		if len(rep.Divergences) != 0 {
			d := rep.Divergences[0]
			t.Fatalf("seed %d: %d divergences, first case %d kind %v",
				seed, len(rep.Divergences), d.CaseID, d.Kind)
		}
		if rep.Equivalent != 200 {
			t.Fatalf("seed %d: equivalent %d of 200", seed, rep.Equivalent)
		}
	}
}

// TestRunConcurrencyConsistency: shard count, concurrency and
// scheduling must not change any verdict.
func TestRunConcurrencyConsistency(t *testing.T) {
	// Generated records contain small integral floats; flip results to
	// false whenever field "a" is 7.0.
	flipOnA7 := func(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) []Outcome {
		out := safeEvalBatch(ctx, n, recs, lim)
		for i, r := range recs {
			if v, ok := r["a"].(float64); ok && v == 7.0 {
				out[i] = Outcome{Val: false}
			}
		}
		return out
	}
	injectBatchFault(t, flipOnA7, func() {
		base := Config{Seed: 11, NumCases: 120, MaxRecords: 8}
		serial := Run(context.Background(), base)
		parallel := Run(context.Background(), Config{
			Seed: 11, NumCases: 120, MaxRecords: 8,
			Shards: 4, Concurrency: 8,
		})
		if serial.Total != parallel.Total || serial.Equivalent != parallel.Equivalent {
			t.Fatalf("counts differ: %+v vs %+v", serial, parallel)
		}
		if len(serial.Divergences) == 0 {
			t.Fatal("fault injection produced no divergences; test is vacuous")
		}
		if len(serial.Divergences) != len(parallel.Divergences) {
			t.Fatalf("divergence counts differ: %d vs %d",
				len(serial.Divergences), len(parallel.Divergences))
		}
		for i := range serial.Divergences {
			a, b := serial.Divergences[i], parallel.Divergences[i]
			if a.CaseID != b.CaseID || a.Kind != b.Kind {
				t.Fatalf("divergence %d differs: %v vs %v", i, a, b)
			}
			if !reflect.DeepEqual(a.Min, b.Min) {
				t.Fatalf("divergence %d minimal cases differ", i)
			}
		}
	})
}

func TestRunTimeoutIsolated(t *testing.T) {
	// Batch side stalls until its own per-case timeout fires.
	injectBatchFault(t, func(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) []Outcome {
		<-ctx.Done()
		out := make([]Outcome, len(recs))
		for i := range out {
			out[i] = Outcome{Timeout: true}
		}
		return out
	}, func() {
		start := time.Now()
		rep := Run(context.Background(), Config{
			Seed: 5, NumCases: 6, MaxRecords: 2,
			CaseTimeout: 30 * time.Millisecond,
			Shards:      2, Concurrency: 2,
		})
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("run took too long: %v", elapsed)
		}
		if len(rep.Divergences) == 0 {
			t.Fatal("expected timeout divergences")
		}
		for _, d := range rep.Divergences {
			if d.Kind != DivTimeout {
				t.Fatalf("want DivTimeout, got %v", d.Kind)
			}
		}
	})
}

func TestRunCrashClassified(t *testing.T) {
	injectBatchFault(t, func(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) []Outcome {
		panic("boom")
	}, func() {
		rep := Run(context.Background(), Config{Seed: 5, NumCases: 10, MaxRecords: 2})
		if len(rep.Divergences) != 10 {
			t.Fatalf("want 10 crash divergences, got %d", len(rep.Divergences))
		}
		for _, d := range rep.Divergences {
			if d.Kind != DivCrash {
				t.Fatalf("want DivCrash, got %v", d.Kind)
			}
		}
	})
}

func TestRunBudgetStops(t *testing.T) {
	// Slow interpreted side: each record evaluation sleeps.
	orig := runInterp
	runInterp = func(ctx context.Context, n expr.Node, rec expr.Record, lim expr.Limits) Outcome {
		select {
		case <-ctx.Done():
			return Outcome{Timeout: true}
		case <-time.After(20 * time.Millisecond):
		}
		return safeEval(ctx, n, rec, lim)
	}
	defer func() { runInterp = orig }()

	start := time.Now()
	rep := Run(context.Background(), Config{
		Seed: 3, NumCases: 500, MaxRecords: 4,
		Budget:      150 * time.Millisecond,
		CaseTimeout: 5 * time.Second,
		Shards:      2, Concurrency: 2,
	})
	elapsed := time.Since(start)
	if !rep.Stopped {
		t.Fatal("run must report early stop under budget")
	}
	if rep.Total >= 500 {
		t.Fatalf("run must stop early, processed %d", rep.Total)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("run did not stop within budget slack: %v", elapsed)
	}
}

func TestRunCancel(t *testing.T) {
	orig := runInterp
	runInterp = func(ctx context.Context, n expr.Node, rec expr.Record, lim expr.Limits) Outcome {
		select {
		case <-ctx.Done():
			return Outcome{Timeout: true}
		case <-time.After(10 * time.Millisecond):
		}
		return safeEval(ctx, n, rec, lim)
	}
	defer func() { runInterp = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *Report, 1)
	go func() { done <- Run(ctx, Config{Seed: 4, NumCases: 500, CaseTimeout: 5 * time.Second}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case rep := <-done:
		if !rep.Stopped {
			t.Fatal("cancelled run must report Stopped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
}

// TestRunLeavesNoPartialState: after timeouts, later cases still
// evaluate cleanly (no poisoned shared state).
func TestRunLeavesNoPartialState(t *testing.T) {
	var calls int
	injectBatchFault(t, func(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) []Outcome {
		calls++
		if calls%2 == 1 { // odd cases stall into timeout
			<-ctx.Done()
			out := make([]Outcome, len(recs))
			for i := range out {
				out[i] = Outcome{Timeout: true}
			}
			return out
		}
		return safeEvalBatch(ctx, n, recs, lim)
	}, func() {
		rep := Run(context.Background(), Config{
			Seed: 8, NumCases: 8, MaxRecords: 2,
			CaseTimeout: 20 * time.Millisecond,
		})
		if rep.Total != 8 {
			t.Fatalf("all cases must complete, got %d", rep.Total)
		}
		if rep.Equivalent == 0 {
			t.Fatal("even cases must still compare equivalent")
		}
	})
}
