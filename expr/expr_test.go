package expr

import "testing"

func TestSmokeBothPathsAgree(t *testing.T) {
	// (and (gt $age 18) (in $tag ["a","b"]))
	e := &Expr{Op: OpAnd,
		Left:  &Expr{Op: OpGt, Left: &Expr{Op: OpField, Name: "age"}, Right: &Expr{Op: OpLit, Lit: Int(18)}},
		Right: &Expr{Op: OpIn, Left: &Expr{Op: OpField, Name: "tag"}, Right: &Expr{Op: OpLit, Lit: List([]Value{Str("a"), Str("b")})}},
	}
	recs := []Record{
		{"age": Int(20), "tag": Str("a")},
		{"age": Int(10), "tag": Str("a")},
		{"age": Int(30), "tag": Str("z")},
		{"age": Null(), "tag": Str("a")},
	}
	br := EvalBatch(e, recs, BatchOptions{})
	for i, rec := range recs {
		v, err := Eval(e, rec, EvalOptions{})
		if (err == nil) != (br.Errs[i] == nil) {
			t.Fatalf("row %d: interp err=%v batch err=%v", i, err, br.Errs[i])
		}
		if err == nil && !v.ExactEqual(br.Values[i]) {
			t.Fatalf("row %d: interp=%v batch=%v", i, v, br.Values[i])
		}
	}
	want := []Value{Bool(true), Bool(false), Bool(false), Null()}
	for i, w := range want {
		if !br.Values[i].ExactEqual(w) {
			t.Fatalf("row %d: got %v want %v", i, br.Values[i], w)
		}
	}
}

func TestDivByZeroClassified(t *testing.T) {
	e := &Expr{Op: OpDiv, Left: &Expr{Op: OpLit, Lit: Int(1)}, Right: &Expr{Op: OpLit, Lit: Int(0)}}
	_, err := Eval(e, Record{}, EvalOptions{})
	if err == nil || err.Class != ErrDivByZero {
		t.Fatalf("want ErrDivByZero, got %v", err)
	}
	br := EvalBatch(e, []Record{{}}, BatchOptions{})
	if br.Errs[0] == nil || br.Errs[0].Class != ErrDivByZero {
		t.Fatalf("batch: want ErrDivByZero, got %v", br.Errs[0])
	}
}

func TestStepBudgetBoundsDeepInput(t *testing.T) {
	// Build a deeply nested expression: not(not(...(true)...))
	deep := &Expr{Op: OpLit, Lit: Bool(true)}
	for i := 0; i < 50000; i++ {
		deep = &Expr{Op: OpNot, Left: deep}
	}
	rec := Record{}
	_, err := Eval(deep, rec, EvalOptions{MaxSteps: 1000})
	if err == nil || err.Class != ErrBudget {
		t.Fatalf("want ErrBudget, got %v", err)
	}
	// Batch path is likewise bounded.
	br := EvalBatch(deep, []Record{rec}, BatchOptions{MaxSteps: 100})
	if br.Errs[0] == nil || br.Errs[0].Class != ErrBudget {
		t.Fatalf("batch: want ErrBudget, got %v", br.Errs[0])
	}
	// Generous budget completes fine.
	v, err := Eval(deep, rec, EvalOptions{MaxSteps: 200000})
	if err != nil || v.Kind != KindBool {
		t.Fatalf("unexpected: %v %v", v, err)
	}
}
