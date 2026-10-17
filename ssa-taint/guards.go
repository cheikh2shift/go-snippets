package main

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// guardClass is the verdict the classifier reaches after reading a guard's SSA
// body. The name got the function into this table; the body decides whether it
// stays.
type guardClass string

const (
	// Cuts taint.
	classEffective     guardClass = "effective"      // delegates to a recognised sanitizer
	classConstant      guardClass = "constant"       // takes no input at all
	classNonForwarding guardClass = "non-forwarding" // returns only error/bool

	// Does not cut taint.
	classPassthrough guardClass = "passthrough" // returns its argument unchanged
	classRewrites    guardClass = "rewrites"    // transforms, but no recognised escaper
	classConditional guardClass = "conditional" // branches and transforms
	classUnknown     guardClass = "unknown"     // no rule matched
)

type guardInfo struct {
	key    key
	fn     *ssa.Function
	class  guardClass
	cuts   bool
	reason string
}

// classifyGuards walks every project function whose name looks like a guard and
// decides, from the body, whether it actually neutralises taint.
//
// This must run before the analysis. An earlier version classified lazily, so
// early call sites resolved to "not a guard yet" and verdicts depended on the
// order the files were walked in.
func classifyGuards(p *program) {
	for _, fn := range p.funcs {
		if !isGuardName(fn.Name()) {
			continue
		}
		gi := classifyGuard(fn)
		p.guards[gi.key] = gi
	}
}

func classifyGuard(fn *ssa.Function) *guardInfo {
	gi := &guardInfo{key: funcKey(fn), fn: fn, class: classUnknown}

	if len(fn.Blocks) == 0 {
		gi.reason = "no SSA body (external or generic instantiation)"
		return gi
	}

	paramSet := map[ssa.Value]bool{}
	for _, prm := range fn.Params {
		paramSet[prm] = true
	}

	var (
		callsSanitizer  bool
		callsPropagator bool
		branches        int
		returnsParam    bool
		resultTypes     []types.Type
	)

	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			switch v := in.(type) {
			case *ssa.Call:
				if kk, ok := callKey(v.Common()); ok {
					if _, isSan := sanitizers[kk]; isSan {
						callsSanitizer = true
					}
					if propagators[kk] {
						callsPropagator = true
					}
				}
			case *ssa.If:
				branches++
			case *ssa.Return:
				for _, r := range v.Results {
					resultTypes = append(resultTypes, r.Type())
					if paramSet[r] {
						returnsParam = true
					}
				}
			}
		}
	}

	allErrOrBool := len(resultTypes) > 0
	for _, t := range resultTypes {
		if !isErrorType(t) && !isBoolType(t) {
			allErrOrBool = false
			break
		}
	}

	switch {
	case len(fn.Params) == 0:
		gi.class = classConstant
		gi.reason = "takes no input, so it cannot forward taint"

	case callsSanitizer && !callsPropagator && branches == 0:
		gi.class = classEffective
		gi.reason = "delegates to a recognised sanitizer and does not transform otherwise"

	case callsSanitizer && (callsPropagator || branches > 0):
		gi.class = classConditional
		gi.reason = "calls a sanitizer but also branches or transforms"

	case allErrOrBool && !returnsParam:
		gi.class = classNonForwarding
		gi.reason = "returns only error/bool values; the input is not forwarded"

	case returnsParam && !callsPropagator && branches == 0:
		gi.class = classPassthrough
		gi.reason = "returns its argument unchanged"

	case callsPropagator && branches == 0:
		gi.class = classRewrites
		gi.reason = "transforms the input without calling a recognised sanitizer"

	case branches > 0:
		gi.class = classConditional
		gi.reason = "branches on the input; the analyser cannot correlate the branch with the sink"

	default:
		gi.class = classUnknown
		gi.reason = "no rule matched"
	}

	gi.cuts = gi.class == classEffective ||
		gi.class == classConstant ||
		gi.class == classNonForwarding
	return gi
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	return named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func isBoolType(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}
