package main

import (
	"strconv"

	"golang.org/x/tools/go/ssa"
)

// taintInfo is what we know about a tainted value: where it came from, and the
// chain of calls it travelled through to get here.
type taintInfo struct {
	origin string
	path   []string
}

// finding is one tainted value arriving at one dangerous argument position.
type finding struct {
	fn     *ssa.Function
	block  int
	instr  ssa.Instruction
	sink   sinkSpec
	argIdx int
	ti     taintInfo
}

func (f finding) severity() string {
	if s, ok := severityOf[f.sink.kind]; ok {
		return s
	}
	return "LOW"
}

// id collapses a finding to a comparable string, so the two passes can be
// diffed without caring about instruction identity.
func (f finding) id() string {
	return f.fn.String() + "|" + f.sink.kind + "|" + strconv.Itoa(f.argIdx) + "|" + f.ti.origin
}

// analyzer runs the taint walk over one function at a time. Interprocedural
// flow is handled by the one-bit summaries in program.summary, not by
// recursion, so a recursive call graph cannot blow the stack.
type analyzer struct {
	p        *program
	tainted  map[ssa.Value]taintInfo
	findings []finding
	seen     map[string]bool
}

func newAnalyzer(p *program) *analyzer {
	return &analyzer{
		p:       p,
		tainted: map[ssa.Value]taintInfo{},
		seen:    map[string]bool{},
	}
}

// set records a value as tainted. It returns true only the first time, which is
// what makes the fixpoint loop terminate.
func (a *analyzer) set(v ssa.Value, ti taintInfo) bool {
	if v == nil {
		return false
	}
	if _, ok := a.tainted[v]; ok {
		return false
	}
	a.tainted[v] = ti
	return true
}

// run walks fn to a fixpoint. When seedParams is true, every parameter starts
// tainted, which is how the interprocedural summary is computed. When record is
// true, sink hits are appended to a.findings.
//
// It returns whether any value reaching a return instruction is tainted.
func (a *analyzer) run(fn *ssa.Function, seedParams bool, record bool) bool {
	a.tainted = map[ssa.Value]taintInfo{}
	if seedParams {
		for _, prm := range fn.Params {
			a.tainted[prm] = taintInfo{
				origin: "parameter " + prm.Name(),
				path:   []string{fn.String()},
			}
		}
	}

	for iter := 0; iter < 200; iter++ {
		changed := false
		for bi, b := range fn.Blocks {
			for _, in := range b.Instrs {
				if a.step(fn, bi, in, record) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			ret, ok := in.(*ssa.Return)
			if !ok {
				continue
			}
			for _, r := range ret.Results {
				if _, tainted := a.tainted[r]; tainted {
					return true
				}
			}
		}
	}
	return false
}

func (a *analyzer) step(fn *ssa.Function, bi int, in ssa.Instruction, record bool) bool {
	switch v := in.(type) {
	case *ssa.Call:
		return a.stepCall(fn, bi, v, v.Common(), record)
	case *ssa.Go:
		return a.stepCall(fn, bi, v, v.Common(), record)
	case *ssa.Defer:
		return a.stepCall(fn, bi, v, v.Common(), record)

	case *ssa.Phi:
		changed := false
		for _, e := range v.Edges {
			if ti, ok := a.tainted[e]; ok {
				changed = a.set(v, ti) || changed
			}
		}
		return changed

	case *ssa.BinOp:
		return a.propagateFrom(v, v.X, v.Y)
	case *ssa.UnOp:
		return a.propagateFrom(v, v.X)
	case *ssa.Convert:
		return a.propagateFrom(v, v.X)
	case *ssa.ChangeType:
		return a.propagateFrom(v, v.X)
	case *ssa.MakeInterface:
		return a.propagateFrom(v, v.X)
	case *ssa.TypeAssert:
		return a.propagateFrom(v, v.X)
	case *ssa.Slice:
		return a.propagateFrom(v, v.X)
	case *ssa.FieldAddr:
		return a.propagateFrom(v, v.X)
	case *ssa.IndexAddr:
		return a.propagateFrom(v, v.X, v.Index)
	case *ssa.Lookup:
		return a.propagateFrom(v, v.X, v.Index)
	case *ssa.Extract:
		return a.propagateFrom(v, v.Tuple)
	case *ssa.Store:
		return a.propagateFrom(v.Addr, v.Val)
	case *ssa.MapUpdate:
		return a.propagateFrom(v.Map, v.Key, v.Value)
	case *ssa.Send:
		return a.propagateFrom(v.Chan, v.X)
	case *ssa.Panic:
		return a.propagateFrom(v.X)
	}
	return false
}

// propagateFrom taints dst if any source is tainted. The first tainted source
// wins, so the reported origin is stable across runs.
func (a *analyzer) propagateFrom(dst ssa.Value, srcs ...ssa.Value) bool {
	for _, s := range srcs {
		if s == nil {
			continue
		}
		if ti, ok := a.tainted[s]; ok {
			return a.set(dst, ti)
		}
	}
	return false
}

// stepCall is the whole rule engine. The order of the checks matters:
//
//	1. source      â€” the call creates taint
//	2. sink        â€” the call consumes taint, and that is a finding
//	3. sanitizer   â€” the result is clean, full stop
//	4. guard       â€” the result is clean only if the body earned it
//	5. propagator  â€” taint passes through
//
// A callee can be both a sink and a propagator (fmt.Sprintf is not, but
// net/url.URL.Query is both a source and a propagator). Steps 1 and 2 do not
// return early, so both fire.
func (a *analyzer) stepCall(fn *ssa.Function, bi int, in ssa.Instruction, c *ssa.CallCommon, record bool) bool {
	changed := false
	results := callResults(in)

	kk, ok := callKey(c)
	if !ok {
		// No static callee: a closure value or a builtin. Conservatively
		// forward taint rather than dropping it.
		if ti, ok := a.firstArgTaint(c); ok {
			for _, r := range results {
				changed = a.set(r, ti) || changed
			}
		}
		return changed
	}

	// 1. source
	if label, isSrc := sourceIndex[kk]; isSrc {
		if !(kk.pkg == "net/http" && !httpSourceOn) {
			ti := taintInfo{origin: label, path: []string{shortName(kk)}}
			for _, r := range results {
				changed = a.set(r, ti) || changed
			}
			return changed
		}
	}

	// 2. sink
	if specs, isSink := sinkIndex[kk]; isSink {
		for _, sp := range specs {
			if sp.arg == -1 {
				for i, arg := range c.Args {
					if ti, ok := a.tainted[arg]; ok && record {
						a.record(fn, bi, in, sp, i, ti)
					}
				}
				continue
			}
			if sp.arg >= 0 && sp.arg < len(c.Args) {
				if ti, ok := a.tainted[c.Args[sp.arg]]; ok && record {
					a.record(fn, bi, in, sp, sp.arg, ti)
				}
			}
		}
	}

	// 3. sanitizer â€” result is clean.
	if _, isSan := sanitizers[kk]; isSan {
		return changed
	}

	// 4. project guard â€” clean only if the classifier said it cuts taint.
	if gi, isGuard := a.p.guards[kk]; isGuard && gi.cuts && !guardsNoop {
		return changed
	}

	// 5. propagator, or a project function whose summary says it taints.
	propagate := propagators[kk]
	if !propagate {
		if callee := c.StaticCallee(); callee != nil && a.p.isProject(callee) {
			propagate = a.p.summary[callee]
		}
	}
	if propagate {
		if ti, ok := a.firstArgTaint(c); ok {
			for _, r := range results {
				changed = a.set(r, ti) || changed
			}
		}
	}
	return changed
}

func (a *analyzer) firstArgTaint(c *ssa.CallCommon) (taintInfo, bool) {
	for _, arg := range c.Args {
		if ti, ok := a.tainted[arg]; ok {
			return ti, true
		}
	}
	return taintInfo{}, false
}

func (a *analyzer) record(fn *ssa.Function, bi int, in ssa.Instruction, sp sinkSpec, argIdx int, ti taintInfo) {
	f := finding{fn: fn, block: bi, instr: in, sink: sp, argIdx: argIdx, ti: ti}
	if a.seen[f.id()] {
		return
	}
	a.seen[f.id()] = true
	a.findings = append(a.findings, f)
}

// callResults returns the value produced by a call instruction. Go and Defer
// produce nothing, and a call to a void function produces nothing either.
func callResults(in ssa.Instruction) []ssa.Value {
	if c, ok := in.(*ssa.Call); ok {
		if v := c.Value(); v != nil {
			return []ssa.Value{v}
		}
	}
	return nil
}

// computeSummaries fills program.summary: for each project function, does any
// parameter taint any result?
//
// This is one bit per function, which is deliberately coarse. A precise model
// would say which parameter reaches which result position, but one bit is
// enough to see a flow cross a helper, and it cannot be wrong in the
// over-reporting direction.
//
// The loop is a fixpoint because summaries depend on each other: wrap2 calls
// wrap1, so wrap2 can only be decided after wrap1 is.
func computeSummaries(p *program) {
	for _, fn := range p.funcs {
		p.summary[fn] = false
	}
	for iter := 0; iter < 16; iter++ {
		changed := false
		for _, fn := range p.funcs {
			a := newAnalyzer(p)
			if a.run(fn, true, false) && !p.summary[fn] {
				p.summary[fn] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}
