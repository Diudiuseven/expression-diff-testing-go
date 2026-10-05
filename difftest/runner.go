package difftest

import (
	"context"
	"sync"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// Divergence records one reproduced divergence. Only the shrunk minimal
// case is retained; the original inputs are released.
type Divergence struct {
	CaseID int64
	Kind   DivKind
	Min    Case // minimal reproducing case
}

// Report summarises a differential run.
type Report struct {
	Total       int // cases fully evaluated
	Equivalent  int
	Divergences []Divergence // sorted by CaseID, independent of scheduling
	Stopped     bool         // true if the run stopped early (budget/cancel)
}

// Run generates cases from cfg, evaluates both paths concurrently across
// shards, and compares outcomes. Shard size, concurrency and scheduling
// never change the equivalence verdict: case i is generated purely from
// (Seed, i) and results are assembled in case-ID order. The run stops
// within cfg.Budget and returns whatever was collected.
func Run(ctx context.Context, cfg Config) *Report {
	cfg = cfg.withDefaults()
	if cfg.Budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Budget)
		defer cancel()
	}
	gen := NewGenerator(cfg)

	// results[i] holds the outcome of case i; nil until processed.
	results := make([]*caseResult, cfg.NumCases)
	var mu sync.Mutex

	var wg sync.WaitGroup
	for s := 0; s < cfg.Shards; s++ {
		jobs := make(chan int)
		// Feeder: case i belongs to shard i%Shards.
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			defer close(jobs)
			for i := s; i < cfg.NumCases; i += cfg.Shards {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case jobs <- i:
				}
			}
		}(s)
		for w := 0; w < cfg.Concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					c := gen.Next(i)
					r := evalCase(ctx, cfg, c)
					if r.kind != DivNone {
						// Confirm, shrink to a minimal case and
						// release the original inputs.
						r.min = ShrinkCtx(ctx, cfg, c, r.kind)
					}
					mu.Lock()
					results[i] = r
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()

	rep := &Report{}
	for i, r := range results {
		if r == nil {
			rep.Stopped = true // case never ran: budget exhausted or cancelled
			continue
		}
		rep.Total++
		if r.kind == DivNone {
			rep.Equivalent++
		} else {
			rep.Divergences = append(rep.Divergences, Divergence{
				CaseID: int64(i),
				Kind:   r.kind,
				Min:    r.min,
			})
		}
	}
	return rep
}

type caseResult struct {
	kind DivKind
	min  Case
}

// evalCase evaluates one case on both paths and returns the first
// divergence kind found (records are compared in order, so the verdict
// is deterministic).
func evalCase(ctx context.Context, cfg Config, c Case) *caseResult {
	a, b := evalBoth(ctx, cfg, c)
	for i := range a {
		if k := Classify(cfg, a[i], b[i]); k != DivNone {
			return &caseResult{kind: k}
		}
	}
	return &caseResult{kind: DivNone}
}

// evalCaseKind is the shrink-time oracle: the divergence kind a case
// still triggers, or DivNone.
func evalCaseKind(ctx context.Context, cfg Config, c Case) DivKind {
	return evalCase(ctx, cfg, c).kind
}

// Path hooks: the two evaluation paths under test. Tests may swap these
// to inject faults (wrong values, panics, stalls) into either side.
var (
	runInterp = safeEval
	runBatch  = safeEvalBatch
)

// evalBoth runs the interpreted and the batch path for the same case.
// Each side runs in its own goroutine with its own timeout context, so
// a slow or stuck side neither blocks nor corrupts the other.
func evalBoth(ctx context.Context, cfg Config, c Case) (interp, batch []Outcome) {
	n := len(c.Records)
	interp = make([]Outcome, n)
	batch = make([]Outcome, n)
	lim := expr.Limits{MaxSteps: cfg.MaxSteps}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // interpreted path
		defer wg.Done()
		i := 0
		defer func() { // a panicking path must not escape its goroutine
			if r := recover(); r != nil {
				for j := i; j < n; j++ {
					interp[j] = Outcome{Paniced: true}
				}
			}
		}()
		cctx, cancel := context.WithTimeout(ctx, cfg.CaseTimeout)
		defer cancel()
		for ; i < n; i++ {
			interp[i] = runInterp(cctx, c.Expr, c.Records[i], lim)
			if cctx.Err() != nil {
				for j := i + 1; j < n; j++ {
					interp[j] = Outcome{Timeout: true}
				}
				return
			}
		}
	}()
	go func() { // batch path
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				for j := range batch {
					batch[j] = Outcome{Paniced: true}
				}
			}
		}()
		cctx, cancel := context.WithTimeout(ctx, cfg.CaseTimeout)
		defer cancel()
		out := runBatch(cctx, c.Expr, c.Records, lim)
		copy(batch, out)
	}()
	wg.Wait()
	return interp, batch
}

// safeEval runs expr.Eval converting panics and deadline expiry into
// Outcome flags; no half-written state escapes.
func safeEval(ctx context.Context, n expr.Node, rec expr.Record, lim expr.Limits) (out Outcome) {
	defer func() {
		if r := recover(); r != nil {
			out = Outcome{Paniced: true}
		}
	}()
	v, err := expr.Eval(ctx, n, rec, lim)
	if err != nil {
		if ctx.Err() != nil {
			return Outcome{Timeout: true}
		}
		return Outcome{Err: err}
	}
	return Outcome{Val: v}
}

// safeEvalBatch is the batch-path analogue of safeEval.
func safeEvalBatch(ctx context.Context, n expr.Node, recs []expr.Record, lim expr.Limits) (out []Outcome) {
	out = make([]Outcome, len(recs))
	defer func() {
		if r := recover(); r != nil {
			for i := range out {
				out[i] = Outcome{Paniced: true}
			}
		}
	}()
	br, err := expr.EvalBatch(ctx, n, recs, lim)
	if err != nil {
		if ctx.Err() != nil {
			for i := range out {
				out[i] = Outcome{Timeout: true}
			}
			return out
		}
		for i := range out {
			out[i] = Outcome{Err: err}
		}
		return out
	}
	for i := range out {
		if br.Errs[i] != nil {
			out[i] = Outcome{Err: br.Errs[i]}
		} else {
			out[i] = Outcome{Val: br.Vals[i]}
		}
	}
	return out
}
