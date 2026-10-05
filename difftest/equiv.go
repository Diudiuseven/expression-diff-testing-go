package difftest

import (
	"math"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// EquivOptions tunes equivalence judgement.
type EquivOptions struct {
	FloatTol float64 // combined relative+absolute tolerance for floats
}

func (o EquivOptions) withDefaults() EquivOptions {
	if o.FloatTol <= 0 {
		o.FloatTol = 1e-9
	}
	return o
}

// Outcome is the classified result of evaluating one case on one path.
type Outcome struct {
	Values []expr.Value  // per-record values when the path succeeded
	Errs   []*expr.Error // per-record classified errors
}

// Equivalent reports whether two outcomes are semantically equal. It
// accounts for float tolerance, null semantics, unordered list (set/group)
// comparison, and error classification: two errors are equivalent iff they
// share the same ErrClass, regardless of message text.
func Equivalent(a, b Outcome, opts EquivOptions) bool {
	opts = opts.withDefaults()
	if len(a.Values) != len(b.Values) || len(a.Errs) != len(b.Errs) {
		return false
	}
	for i := range a.Values {
		ae, be := a.Errs[i], b.Errs[i]
		if (ae == nil) != (be == nil) {
			return false
		}
		if ae != nil {
			if ae.Class != be.Class {
				return false
			}
			continue
		}
		if !valueEquiv(a.Values[i], b.Values[i], opts.FloatTol) {
			return false
		}
	}
	return true
}

// valueEquiv compares two values: null equals null; ints and floats are
// compared numerically within tolerance; lists are compared as unordered
// multisets so that set/group results with different orderings still match.
func valueEquiv(a, b expr.Value, tol float64) bool {
	if a.Kind == expr.KindNull || b.Kind == expr.KindNull {
		return a.Kind == b.Kind
	}
	an, aNum := asFloat(a)
	bn, bNum := asFloat(b)
	if aNum || bNum {
		if !aNum || !bNum {
			return false
		}
		return floatsEquiv(an, bn, tol)
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case expr.KindBool:
		return a.B == b.B
	case expr.KindString:
		return a.S == b.S
	case expr.KindList:
		return listMultisetEquiv(a.L, b.L, tol)
	}
	return false
}

func asFloat(v expr.Value) (float64, bool) {
	switch v.Kind {
	case expr.KindInt:
		return float64(v.I), true
	case expr.KindFloat:
		return v.F, true
	}
	return 0, false
}

// floatsEquiv uses a combined absolute+relative tolerance.
func floatsEquiv(a, b, tol float64) bool {
	if a == b {
		return true
	}
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	diff := math.Abs(a - b)
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return diff <= tol*scale
}

// listMultisetEquiv compares lists as unordered multisets with tolerance,
// using deterministic greedy matching over a sorted copy.
func listMultisetEquiv(a, b []expr.Value, tol float64) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		found := false
		for j, y := range b {
			if !used[j] && valueEquiv(x, y, tol) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
