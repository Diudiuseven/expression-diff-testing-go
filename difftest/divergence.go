package difftest

// DivKind classifies a divergence between the two evaluation paths.
// Each category is reported separately; they are never merged.
type DivKind int

const (
	DivNone       DivKind = iota
	DivValue              // both produced values but they differ
	DivValueVsErr         // one side returned a value, the other an error
	DivErrClass           // both errored but with different error classes
	DivTimeout            // one side exceeded its time budget
	DivCrash              // one side panicked
)

func (k DivKind) String() string {
	switch k {
	case DivNone:
		return "none"
	case DivValue:
		return "value-mismatch"
	case DivValueVsErr:
		return "value-vs-error"
	case DivErrClass:
		return "error-class-mismatch"
	case DivTimeout:
		return "timeout"
	case DivCrash:
		return "crash"
	}
	return "unknown"
}

// Divergence describes one detected disagreement.
type Divergence struct {
	Kind DivKind
	Case Case
	Row  int // record index where the divergence was observed
}

// Classify compares two outcomes and returns the divergence kind and the
// first row exhibiting it. Timeout and crash are reported by the runner
// directly; Classify covers value/error divergences only.
func Classify(a, b Outcome, opts EquivOptions) (DivKind, int) {
	opts = opts.withDefaults()
	n := len(a.Values)
	if len(b.Values) < n {
		n = len(b.Values)
	}
	for i := 0; i < n; i++ {
		ae, be := a.Errs[i], b.Errs[i]
		switch {
		case ae == nil && be == nil:
			if !valueEquiv(a.Values[i], b.Values[i], opts.FloatTol) {
				return DivValue, i
			}
		case ae != nil && be != nil:
			if ae.Class != be.Class {
				return DivErrClass, i
			}
		default:
			return DivValueVsErr, i
		}
	}
	if len(a.Values) != len(b.Values) {
		return DivValue, n
	}
	return DivNone, -1
}
