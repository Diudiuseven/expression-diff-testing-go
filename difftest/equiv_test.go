package difftest

import (
	"testing"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

func eqCfg() Config { return Config{FloatTol: 1e-6}.withDefaults() }

func TestEquivalentFloatTolerance(t *testing.T) {
	cfg := eqCfg()
	a := Outcome{Val: 1.0000001}
	b := Outcome{Val: 1.0000002}
	if !Equivalent(cfg, a, b) {
		t.Fatal("floats within tolerance must be equivalent")
	}
	c := Outcome{Val: 1.1}
	if Equivalent(cfg, a, c) {
		t.Fatal("floats beyond tolerance must diverge")
	}
	if k := Classify(cfg, a, c); k != DivValue {
		t.Fatalf("want DivValue, got %v", k)
	}
}

func TestEquivalentNullSemantics(t *testing.T) {
	cfg := eqCfg()
	if !Equivalent(cfg, Outcome{Val: nil}, Outcome{Val: nil}) {
		t.Fatal("nil must equal nil")
	}
	if Equivalent(cfg, Outcome{Val: nil}, Outcome{Val: 0.0}) {
		t.Fatal("nil must not equal 0.0")
	}
	if Equivalent(cfg, Outcome{Val: nil}, Outcome{Val: false}) {
		t.Fatal("nil must not equal false")
	}
}

func TestEquivalentUnorderedSets(t *testing.T) {
	cfg := eqCfg()
	a := Outcome{Val: []any{1.0, "x", nil}}
	b := Outcome{Val: []any{nil, 1.0 + 1e-9, "x"}}
	if !Equivalent(cfg, a, b) {
		t.Fatal("set results must compare unordered with tolerance")
	}
	c := Outcome{Val: []any{1.0, "x"}}
	if Equivalent(cfg, a, c) {
		t.Fatal("different cardinality must diverge")
	}
	d := Outcome{Val: []any{1.0, 1.0, "x"}}
	if Equivalent(cfg, Outcome{Val: []any{1.0, 1.0, "x"}}, b) == Equivalent(cfg, d, b) {
		_ = d
	}
}

func TestClassifyErrorKinds(t *testing.T) {
	cfg := eqCfg()
	e1 := &expr.Error{Kind: expr.ErrDivByZero, Msg: "x"}
	e2 := &expr.Error{Kind: expr.ErrTypeMismatch, Msg: "y"}
	e3 := &expr.Error{Kind: expr.ErrDivByZero, Msg: "different message"}

	if k := Classify(cfg, Outcome{Err: e1}, Outcome{Err: e3}); k != DivNone {
		t.Fatalf("same error kind with different messages must be equivalent, got %v", k)
	}
	if k := Classify(cfg, Outcome{Err: e1}, Outcome{Err: e2}); k != DivErrorKind {
		t.Fatalf("want DivErrorKind, got %v", k)
	}
	if k := Classify(cfg, Outcome{Err: e1}, Outcome{Val: true}); k != DivErrorVsValue {
		t.Fatalf("want DivErrorVsValue, got %v", k)
	}
}

func TestClassifyTimeoutAndCrash(t *testing.T) {
	cfg := eqCfg()
	if k := Classify(cfg, Outcome{Timeout: true}, Outcome{Val: true}); k != DivTimeout {
		t.Fatalf("want DivTimeout, got %v", k)
	}
	if k := Classify(cfg, Outcome{Paniced: true}, Outcome{Err: nil, Val: true}); k != DivCrash {
		t.Fatalf("want DivCrash, got %v", k)
	}
	if k := Classify(cfg, Outcome{Timeout: true}, Outcome{Paniced: true}); k != DivTimeout {
		t.Fatalf("timeout takes precedence, got %v", k)
	}
	// Both sides failing the same way is not a divergence.
	if k := Classify(cfg, Outcome{Timeout: true}, Outcome{Timeout: true}); k != DivNone {
		t.Fatalf("both timed out must be DivNone, got %v", k)
	}
	if k := Classify(cfg, Outcome{Paniced: true}, Outcome{Paniced: true}); k != DivNone {
		t.Fatalf("both panicked must be DivNone, got %v", k)
	}
}

func TestDivKindStringsDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range []DivKind{DivValue, DivErrorVsValue, DivErrorKind, DivTimeout, DivCrash} {
		s := k.String()
		if s == "" || s == "unknown" || seen[s] {
			t.Fatalf("kind %d has bad or duplicate string %q", int(k), s)
		}
		seen[s] = true
	}
}
