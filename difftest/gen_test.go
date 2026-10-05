package difftest

import (
	"reflect"
	"testing"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

func TestSeedReproducibility(t *testing.T) {
	cfg := Config{Seed: 42, NumCases: 50, MaxDepth: 5, MaxRecords: 8}
	g1 := NewGenerator(cfg)
	g2 := NewGenerator(cfg)
	for i := 0; i < 50; i++ {
		c1, c2 := g1.Next(i), g2.Next(i)
		if !reflect.DeepEqual(c1, c2) {
			t.Fatalf("case %d differs between runs with same seed", i)
		}
	}
}

func TestSeedOrderIndependence(t *testing.T) {
	cfg := Config{Seed: 7, MaxDepth: 4, MaxRecords: 4}
	g := NewGenerator(cfg)
	// Case i must be identical no matter what was generated before.
	warm := NewGenerator(cfg)
	for i := 0; i < 10; i++ {
		warm.Next(i)
	}
	for _, i := range []int{0, 5, 9} {
		a, b := g.Next(i), warm.Next(i)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("case %d depends on generation order", i)
		}
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	g1 := NewGenerator(Config{Seed: 1})
	g2 := NewGenerator(Config{Seed: 2})
	same := 0
	for i := 0; i < 20; i++ {
		if reflect.DeepEqual(g1.Next(i), g2.Next(i)) {
			same++
		}
	}
	if same == 20 {
		t.Fatal("different seeds produced identical case streams")
	}
}

func TestGeneratorResourceBounds(t *testing.T) {
	cfg := Config{Seed: 99, NumCases: 100, MaxDepth: 4, MaxRecords: 6}
	g := NewGenerator(cfg)
	for i := 0; i < 100; i++ {
		c := g.Next(i)
		if d := expr.Depth(c.Expr); d > cfg.MaxDepth+1 {
			t.Fatalf("case %d depth %d exceeds bound", i, d)
		}
		if len(c.Records) > cfg.MaxRecords {
			t.Fatalf("case %d has %d records > %d", i, len(c.Records), cfg.MaxRecords)
		}
	}
}
