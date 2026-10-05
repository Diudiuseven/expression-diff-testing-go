package difftest

import (
	"context"
	"testing"
	"time"
)

func TestSweepManySeeds(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		cfg := RunConfig{
			Gen:         GenConfig{Seed: seed, NumCases: 500, MaxDepth: 7, MaxRecords: 10},
			Workers:     8,
			CaseTimeout: 5 * time.Second,
			Budget:      120 * time.Second,
		}
		rep := Run(context.Background(), cfg)
		for _, d := range rep.Divergences {
			t.Errorf("seed %d case %d: %v expr=%s", seed, d.Case.ID, d.Kind, d.Case.Expr.String())
		}
	}
}
