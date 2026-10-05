// Package difftest provides differential testing between the interpreted
// (expr.Eval) and the optimized batch (expr.EvalBatch) evaluation paths.
package difftest

import "time"

// Config controls case generation, equivalence and execution.
type Config struct {
	Seed        int64         // fixed seed; same seed+config reproduces identical cases
	NumCases    int           // number of cases to generate
	MaxDepth    int           // max expression nesting depth (resource bound)
	MaxRecords  int           // max records per case (resource bound)
	FloatTol    float64       // absolute tolerance for float equivalence
	Shards      int           // number of shards; 0/1 means single shard
	Concurrency int           // workers per shard; 0/1 means serial
	CaseTimeout time.Duration // per-case timeout; 0 means no timeout
	Budget      time.Duration // overall run budget; 0 means no budget
	MaxSteps    int           // per-evaluation step limit; 0 means unlimited
}

// withDefaults fills zero fields with sane defaults.
func (c Config) withDefaults() Config {
	if c.NumCases <= 0 {
		c.NumCases = 100
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = 6
	}
	if c.MaxRecords <= 0 {
		c.MaxRecords = 16
	}
	if c.FloatTol <= 0 {
		c.FloatTol = 1e-9
	}
	if c.Shards <= 0 {
		c.Shards = 1
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	if c.CaseTimeout <= 0 {
		c.CaseTimeout = 2 * time.Second
	}
	return c
}
