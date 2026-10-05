package expr

import (
	"context"
	"reflect"
	"testing"
)

func runBoth(t *testing.T, n Node, recs []Record) {
	t.Helper()
	ctx := context.Background()
	br, err := EvalBatch(ctx, n, recs, Limits{})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	for i, rec := range recs {
		v, err := Eval(ctx, n, rec, Limits{})
		if (err == nil) != (br.Errs[i] == nil) {
			t.Fatalf("rec %d: interp err=%v batch err=%v", i, err, br.Errs[i])
		}
		if err != nil {
			ie, be := err.(*Error), br.Errs[i].(*Error)
			if ie.Kind != be.Kind {
				t.Fatalf("rec %d: err kind %v vs %v", i, ie.Kind, be.Kind)
			}
			continue
		}
		if !reflect.DeepEqual(v, br.Vals[i]) {
			t.Fatalf("rec %d: interp %v batch %v", i, v, br.Vals[i])
		}
	}
}

func TestSmokeAndOrShortCircuit(t *testing.T) {
	n := &And{L: &Cmp{Op: OpGt, L: &Field{Name: "x"}, R: &Lit{Val: 1.0}},
		R: &Or{L: &Field{Name: "flag"}, R: &Not{X: &Field{Name: "flag"}}}}
	recs := []Record{
		{"x": 2.0, "flag": true},
		{"x": 0.5},               // short-circuits, flag missing is fine
		{"x": 3.0, "flag": "no"}, // type mismatch
		{"x": nil, "flag": true}, // null arithmetic
		{"y": 1.0},               // unknown field x
	}
	runBoth(t, n, recs)
}

func TestSmokeArithIn(t *testing.T) {
	n := &In{X: &Arith{Op: OpDiv, L: &Field{Name: "a"}, R: &Lit{Val: 2.0}},
		List: []Node{&Lit{Val: 1.5}, &Lit{Val: 0.0}, &Field{Name: "b"}}}
	recs := []Record{
		{"a": 3.0, "b": 9.0},
		{"a": 0.0, "b": 1.0},
		{"a": nil, "b": 1.0},
		{"a": "s", "b": 1.0},
	}
	runBoth(t, n, recs)
}

func TestSmokeConstantFold(t *testing.T) {
	n := &Cmp{Op: OpEq, L: &Arith{Op: OpMul, L: &Lit{Val: 2.0}, R: &Lit{Val: 3.0}}, R: &Lit{Val: 6.0}}
	runBoth(t, n, []Record{{}, {"x": 1.0}})
}

func TestSmokeStepLimit(t *testing.T) {
	n := &And{L: &Lit{Val: true}, R: &And{L: &Lit{Val: true}, R: &Lit{Val: true}}}
	ctx := context.Background()
	_, err := Eval(ctx, n, Record{}, Limits{MaxSteps: 2})
	e, ok := err.(*Error)
	if !ok || e.Kind != ErrStepLimit {
		t.Fatalf("want step limit, got %v", err)
	}
	br, berr := EvalBatch(ctx, n, []Record{{}}, Limits{MaxSteps: 2})
	if berr != nil {
		t.Fatal(berr)
	}
	be, ok := br.Errs[0].(*Error)
	if !ok || be.Kind != ErrStepLimit {
		t.Fatalf("batch want step limit, got %v", br.Errs[0])
	}
}
