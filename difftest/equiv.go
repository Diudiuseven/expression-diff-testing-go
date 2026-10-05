package difftest

import (
	"errors"
	"math"

	"github.com/Diudiuseven/expression-diff-testing-go/expr"
)

// DivKind classifies a divergence between the two evaluation paths.
type DivKind int

const (
	DivNone         DivKind = iota // outcomes are equivalent
	DivValue                       // both returned values, but they differ
	DivErrorVsValue                // one side errored, the other returned a value
	DivErrorKind                   // both errored, with different error kinds
	DivTimeout                     // exactly one side timed out
	DivCrash                       // exactly one side panicked
)

func (k DivKind) String() string {
	switch k {
	case DivNone:
		return "none"
	case DivValue:
		return "value-mismatch"
	case DivErrorVsValue:
		return "error-vs-value"
	case DivErrorKind:
		return "error-kind-mismatch"
	case DivTimeout:
		return "timeout"
	case DivCrash:
		return "crash"
	}
	return "unknown"
}

// Outcome is the result of running one path on one input.
type Outcome struct {
	Val     any
	Err     error
	Paniced bool
	Timeout bool
}

// kindUnknown is the sentinel error kind for non-classified errors.
const kindUnknown = expr.ErrKind(-1)

// Equivalent reports whether two outcomes are equivalent under cfg:
// float tolerance, null semantics, unordered set/group comparison and
// error-kind classification are all accounted for.
func Equivalent(cfg Config, a, b Outcome) bool {
	return Classify(cfg, a, b) == DivNone
}

// Classify returns the divergence kind between two outcomes. Timeout and
// crash divergences take precedence over value/error comparison; when
// both sides share the same failure status their values/errors are
// still compared.
func Classify(cfg Config, a, b Outcome) DivKind {
	if a.Timeout != b.Timeout {
		return DivTimeout
	}
	if a.Paniced != b.Paniced {
		return DivCrash
	}
	if a.Timeout && b.Timeout {
		return DivNone // both sides blew the same budget
	}
	if a.Paniced && b.Paniced {
		return DivNone
	}
	if (a.Err != nil) != (b.Err != nil) {
		return DivErrorVsValue
	}
	if a.Err != nil && b.Err != nil {
		if errKindOf(a.Err) != errKindOf(b.Err) {
			return DivErrorKind
		}
		return DivNone
	}
	if !valuesEqual(cfg.FloatTol, a.Val, b.Val) {
		return DivValue
	}
	return DivNone
}

// errKindOf extracts the expr.ErrKind of an error (ErrNone for nil).
func errKindOf(err error) expr.ErrKind {
	if err == nil {
		return expr.ErrNone
	}
	var ee *expr.Error
	if errors.As(err, &ee) {
		return ee.Kind
	}
	return kindUnknown
}

// valuesEqual compares two results. Floats compare within tol, nil
// equals nil, and slices compare as unordered multisets (set/group
// results have no significant order).
func valuesEqual(tol float64, a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false
		}
		if math.IsNaN(av) || math.IsNaN(bv) {
			return math.IsNaN(av) && math.IsNaN(bv)
		}
		return av == bv || math.Abs(av-bv) <= tol
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		used := make([]bool, len(bv))
	outer:
		for _, x := range av {
			for j, y := range bv {
				if !used[j] && valuesEqual(tol, x, y) {
					used[j] = true
					continue outer
				}
			}
			return false
		}
		return true
	}
	return false
}
