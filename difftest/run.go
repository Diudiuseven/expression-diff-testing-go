package difftest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// RunConfig controls execution of a differential run.
type RunConfig struct {
	Gen         GenConfig
	Equiv       EquivOptions
	ShardSize   int           // cases per shard
	Workers     int           // concurrent shard workers
	CaseTimeout time.Duration // per-case timeout
	Budget      time.Duration // overall wall-clock budget
}

func (c RunConfig) withDefaults() RunConfig {
	if c.ShardSize <= 0 {
		c.ShardSize = 16
	}
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.CaseTimeout <= 0 {
		c.CaseTimeout = 2 * time.Second
	}
	if c.Budget <= 0 {
		c.Budget = 30 * time.Second
	}
	c.Gen = c.Gen.withDefaults()
	return c
}

// Report summarizes a run.
type Report struct {
	Total       int // cases actually evaluated
	Divergences []Divergence
	Stopped     bool // true if the budget was exhausted before completion
}

// sideResult is the outcome of one evaluation path for one case.
type sideResult struct {
	out      Outcome
	crashed  bool
	timedOut bool
}

// Run executes the differential campaign and returns collected conclusions.
// It always terminates within cfg.Budget; whatever was collected before the
// deadline is reported. Shard size, worker count and scheduling order do
// not affect equivalence judgements: divergences are keyed by case ID and
// sorted deterministically.
func Run(ctx context.Context, cfg RunConfig) Report {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, cfg.Budget)
	defer cancel()

	// Cases are generated lazily by the feeder so that a huge NumCases
	// cannot exhaust memory and generation stops with the budget.
	gen := NewGenerator(cfg.Gen)

	jobs := make(chan []Case)
	var mu sync.Mutex
	var divs []Divergence
	evaluated := 0
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for shard := range jobs {
			for _, c := range shard {
				if ctx.Err() != nil {
					return
				}
				d, found := runCase(ctx, c, cfg)
				mu.Lock()
				evaluated++
				if found {
					divs = append(divs, d)
				}
				mu.Unlock()
			}
		}
	}
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go worker()
	}
	// Feeder: stream shards to workers until generation is exhausted or
	// the budget/context is done.
feed:
	for {
		shard := make([]Case, 0, cfg.ShardSize)
		for len(shard) < cfg.ShardSize {
			c := gen.Next()
			if c == nil {
				break
			}
			shard = append(shard, *c)
		}
		if len(shard) == 0 {
			break
		}
		select {
		case jobs <- shard:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(divs, func(i, j int) bool { return divs[i].Case.ID < divs[j].Case.ID })
	return Report{
		Total:       evaluated,
		Divergences: divs,
		Stopped:     ctx.Err() != nil && evaluated < cfg.Gen.NumCases,
	}
}

// runCase evaluates one case on both paths, each isolated in its own
// goroutine with its own timeout so a slow side can neither block nor
// pollute the other, and classifies any divergence.
func runCase(ctx context.Context, c Case, cfg RunConfig) (Divergence, bool) {
	interpCh := runSide(ctx, cfg.CaseTimeout, func() Outcome {
		var o Outcome
		for _, rec := range c.Records {
			v, err := expr.Eval(c.Expr, rec, expr.EvalOptions{})
			o.Values = append(o.Values, v)
			o.Errs = append(o.Errs, err)
		}
		return o
	})
	batchCh := runSide(ctx, cfg.CaseTimeout, func() Outcome {
		br := expr.EvalBatch(c.Expr, c.Records, expr.BatchOptions{})
		return Outcome{Values: br.Values, Errs: br.Errs}
	})
	a, b := <-interpCh, <-batchCh

	kind := DivNone
	row := -1
	switch {
	case a.timedOut || b.timedOut:
		kind = DivTimeout
	case a.crashed || b.crashed:
		kind = DivCrash
	default:
		kind, row = Classify(a.out, b.out, cfg.Equiv)
	}
	if kind == DivNone {
		return Divergence{}, false
	}

	// Confirm and minimize: only the minimal reproducing case is retained.
	sh := Shrinker{Equiv: cfg.Equiv, Check: func(cand Case) DivKind {
		k, _ := classifyTimed(ctx, cand, cfg)
		return k
	}}
	minimal := sh.Shrink(c, kind)
	return Divergence{Kind: kind, Case: minimal, Row: row}, true
}

func classifyTimed(ctx context.Context, c Case, cfg RunConfig) (DivKind, int) {
	interpCh := runSide(ctx, cfg.CaseTimeout, func() Outcome {
		var o Outcome
		for _, rec := range c.Records {
			v, err := expr.Eval(c.Expr, rec, expr.EvalOptions{})
			o.Values = append(o.Values, v)
			o.Errs = append(o.Errs, err)
		}
		return o
	})
	batchCh := runSide(ctx, cfg.CaseTimeout, func() Outcome {
		br := expr.EvalBatch(c.Expr, c.Records, expr.BatchOptions{})
		return Outcome{Values: br.Values, Errs: br.Errs}
	})
	a, b := <-interpCh, <-batchCh
	switch {
	case a.timedOut || b.timedOut:
		return DivTimeout, -1
	case a.crashed || b.crashed:
		return DivCrash, -1
	}
	return Classify(a.out, b.out, cfg.Equiv)
}

// runSide executes fn in its own goroutine with panic recovery and a
// timeout. Abandoned goroutines hold no shared state, so a timed-out side
// cannot corrupt subsequent cases.
func runSide(ctx context.Context, timeout time.Duration, fn func() Outcome) <-chan sideResult {
	ch := make(chan sideResult, 1)
	go func() {
		var res sideResult
		defer func() {
			if r := recover(); r != nil {
				res.crashed = true
				res.out = Outcome{}
			}
			ch <- res
		}()
		res.out = fn()
	}()
	out := make(chan sideResult, 1)
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case r := <-ch:
			out <- r
		case <-timer.C:
			out <- sideResult{timedOut: true}
		case <-ctx.Done():
			out <- sideResult{timedOut: true}
		}
	}()
	return out
}
