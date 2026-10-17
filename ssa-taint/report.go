package main

import (
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/tools/go/ssa"
)

// runResult carries everything main.go measured, so report.go can be a pure
// formatting layer.
type runResult struct {
	loadDur    time.Duration
	guardDur   time.Duration
	summaryDur time.Duration
	pass1Dur   time.Duration
	pass2Dur   time.Duration
	probeDur   time.Duration

	findings []finding
	guards   []*guardInfo
	layout   []layoutRow
	second   []finding
}

func (r *runResult) total() time.Duration {
	return r.loadDur + r.guardDur + r.summaryDur + r.pass1Dur + r.pass2Dur + r.probeDur
}

func (r *runResult) suspiciousRows() int {
	n := 0
	for _, row := range r.layout {
		if row.suspicious {
			n++
		}
	}
	return n
}

func posOf(p *program, in ssa.Instruction) string {
	if in == nil || in.Pos() == token.NoPos {
		return "-"
	}
	pos := p.prog.Fset.Position(in.Pos())
	return fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line)
}

// trimFn shortens a fully qualified function name to package.Function.
func trimFn(fn *ssa.Function) string {
	s := fn.String()
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func printReport(w io.Writer, p *program, r *runResult) {
	rule := strings.Repeat("=", 78)
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w, "  LEAKCHECK — SSA taint analysis for Go")
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)

	printTiming(w, r)
	printProgram(w, p, r)
	printFindings(w, r)
	printPaths(w, p, r)
	printGuards(w, r)
	printLayout(w, r)
	printSecond(w, r)
	printCaveats(w)
}

func printTiming(w io.Writer, r *runResult) {
	fmt.Fprintln(w, "TIMING")
	rows := [][2]string{
		{"load + SSA build", r.loadDur.String()},
		{"guard classification", r.guardDur.String()},
		{"summary fixpoint", r.summaryDur.String()},
		{"pass 1 (guards trusted)", r.pass1Dur.String()},
		{"pass 2 (guards no-op)", r.pass2Dur.String()},
		{"layout probe", r.probeDur.String()},
		{"total", r.total().String()},
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %-26s %s\n", row[0], row[1])
	}
	fmt.Fprintln(w)
}

func printProgram(w io.Writer, p *program, r *runResult) {
	tainting := 0
	for _, v := range p.summary {
		if v {
			tainting++
		}
	}
	fmt.Fprintln(w, "PROGRAM")
	fmt.Fprintf(w, "  %-26s %d\n", "packages analysed", len(p.project))
	fmt.Fprintf(w, "  %-26s %d\n", "functions", len(p.funcs))
	fmt.Fprintf(w, "  %-26s %d\n", "SSA instructions", p.countInstructions())
	fmt.Fprintf(w, "  %-26s %d\n", "guards classified", len(p.guards))
	fmt.Fprintf(w, "  %-26s %d\n", "callees indexed", len(p.declared))
	fmt.Fprintf(w, "  %-26s %d\n", "summaries that taint", tainting)
	fmt.Fprintln(w)
}

func printFindings(w io.Writer, r *runResult) {
	fmt.Fprintf(w, "FINDINGS (pass 1 — guards trusted) — %d\n", len(r.findings))
	if len(r.findings) == 0 {
		fmt.Fprintln(w, "  (none)")
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "  %-3s %-8s %-19s %-30s %-5s %s\n",
		"#", "SEVERITY", "KIND", "FUNCTION", "ARG", "ORIGIN")
	for i, f := range r.findings {
		fmt.Fprintf(w, "  %-3d %-8s %-19s %-30s %-5d %s\n",
			i+1, f.severity(), f.sink.kind, trimFn(f.fn), f.argIdx, f.ti.origin)
	}
	fmt.Fprintln(w)
}

func printPaths(w io.Writer, p *program, r *runResult) {
	fmt.Fprintln(w, "TAINT PATHS")
	if len(r.findings) == 0 {
		fmt.Fprintln(w, "  (none)")
		fmt.Fprintln(w)
		return
	}
	for i, f := range r.findings {
		fmt.Fprintf(w, "  [%d] %s  (%s)\n", i+1, trimFn(f.fn), posOf(p, f.instr))
		fmt.Fprintf(w, "      origin : %s\n", f.ti.origin)
		fmt.Fprintf(w, "      sink   : %s  arg %d  (%s)\n",
			f.sink.key.String(), f.argIdx, f.sink.kind)
		if len(f.ti.path) > 0 {
			fmt.Fprintf(w, "      path   : %s\n", strings.Join(f.ti.path, " -> "))
		}
	}
	fmt.Fprintln(w)
}

func printGuards(w io.Writer, r *runResult) {
	fmt.Fprintf(w, "GUARD EFFECTIVENESS — %d classified\n", len(r.guards))
	fmt.Fprintf(w, "  %-36s %-16s %-6s %s\n", "FUNCTION", "CLASS", "CUTS", "REASON")
	for _, gi := range r.guards {
		cuts := "no"
		if gi.cuts {
			cuts = "yes"
		}
		fmt.Fprintf(w, "  %-36s %-16s %-6s %s\n", trimFn(gi.fn), gi.class, cuts, gi.reason)
	}
	fmt.Fprintln(w)
}

func printLayout(w io.Writer, r *runResult) {
	fmt.Fprintf(w, "ARGUMENT LAYOUT (probe) — %d rule(s), %d suspicious\n",
		len(r.layout), r.suspiciousRows())
	fmt.Fprintf(w, "  %-40s %-6s %-7s %-20s %s\n",
		"CALLEE", "RULE", "PARAMS", "DECLARED TYPE", "NOTE")
	for _, row := range r.layout {
		ruleArg := "any"
		if row.ruleArg >= 0 {
			ruleArg = fmt.Sprintf("%d", row.ruleArg)
		}
		params := "-"
		if row.resolved {
			params = fmt.Sprintf("%d", row.params)
		}
		dt := row.declType
		if dt == "" {
			dt = "-"
		}
		note := row.note
		if row.suspicious {
			note = "SUSPICIOUS: " + note
		}
		fmt.Fprintf(w, "  %-40s %-6s %-7s %-20s %s\n",
			row.callee.String(), ruleArg, params, dt, note)
	}
	fmt.Fprintln(w)
}

func printSecond(w io.Writer, r *runResult) {
	delta := len(r.second) - len(r.findings)
	fmt.Fprintln(w, "SECOND PASS — every project guard treated as a no-op")
	fmt.Fprintf(w, "  %-34s %d\n", "findings with guards", len(r.findings))
	fmt.Fprintf(w, "  %-34s %d\n", "findings without guards", len(r.second))
	fmt.Fprintf(w, "  %-34s %d\n", "flows that depend on a guard", delta)

	if delta > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Flows that appear only when guards are removed:")
		seen := map[string]bool{}
		for _, f := range r.findings {
			seen[f.id()] = true
		}
		for _, f := range r.second {
			if seen[f.id()] {
				continue
			}
			fmt.Fprintf(w, "    %-30s %-19s arg %d  origin %s\n",
				trimFn(f.fn), f.sink.kind, f.argIdx, f.ti.origin)
		}
	}
	fmt.Fprintln(w)
}

func printCaveats(w io.Writer) {
	fmt.Fprintln(w, "CAVEATS")
	for _, c := range []string{
		"Sanitizers are not checked against sink kind. url.QueryEscape stops taint at a SQL sink, which is wrong. This is planted false negative FN-1.",
		"Variadic sinks use arg == -1, so every argument is treated as dangerous. exec.Command(\"echo\", name) is reported even though the command is constant. Documented false positive.",
		"filepath.Base and friends are propagators, not sanitizers, so CleanGuard is reported. Documented false positive.",
		"Predicate guards are not correlated with sinks. ValidatePath(name) protects only the branch that consults it; the analyser does not track that, so a handler that reads the raw value is still reported.",
		"Interprocedural summaries are one bit per function: 'some parameter taints some result'. They cannot say which parameter or which result.",
		"Only functions in the analysed packages are summarised. A taint path through a dependency is invisible unless the dependency is in the load patterns.",
	} {
		fmt.Fprintf(w, "  - %s\n", c)
	}
	fmt.Fprintln(w)
}

// ---------------------------------------------------------------- JSON

type jsonFinding struct {
	Function string   `json:"function"`
	Position string   `json:"position"`
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"`
	ArgIndex int      `json:"arg_index"`
	Origin   string   `json:"origin"`
	Path     []string `json:"path,omitempty"`
}

type jsonGuard struct {
	Function string `json:"function"`
	Class    string `json:"class"`
	Cuts     bool   `json:"cuts"`
	Reason   string `json:"reason"`
}

type jsonLayout struct {
	Callee     string `json:"callee"`
	RuleArg    int    `json:"rule_arg"`
	Params     int    `json:"params"`
	Resolved   bool   `json:"resolved"`
	DeclType   string `json:"declared_type,omitempty"`
	Note       string `json:"note"`
	Suspicious bool   `json:"suspicious"`
}

type jsonReport struct {
	Packages        int               `json:"packages"`
	Functions       int               `json:"functions"`
	Instructions    int               `json:"ssa_instructions"`
	Findings        []jsonFinding     `json:"findings"`
	Guards          []jsonGuard       `json:"guards"`
	Layout          []jsonLayout      `json:"layout"`
	SuspiciousRows  int               `json:"suspicious_rows"`
	SecondPassCount int               `json:"second_pass_findings"`
	GuardDependent  int               `json:"guard_dependent_flows"`
	Timing          map[string]string `json:"timing"`
}

func writeJSON(path string, p *program, r *runResult) error {
	rep := jsonReport{
		Packages:        len(p.project),
		Functions:       len(p.funcs),
		Instructions:    p.countInstructions(),
		SuspiciousRows:  r.suspiciousRows(),
		SecondPassCount: len(r.second),
		GuardDependent:  len(r.second) - len(r.findings),
		Timing: map[string]string{
			"load":    r.loadDur.String(),
			"guards":  r.guardDur.String(),
			"summary": r.summaryDur.String(),
			"pass1":   r.pass1Dur.String(),
			"pass2":   r.pass2Dur.String(),
			"probe":   r.probeDur.String(),
			"total":   r.total().String(),
		},
	}
	for _, f := range r.findings {
		rep.Findings = append(rep.Findings, jsonFinding{
			Function: f.fn.String(),
			Position: posOf(p, f.instr),
			Kind:     f.sink.kind,
			Severity: f.severity(),
			ArgIndex: f.argIdx,
			Origin:   f.ti.origin,
			Path:     f.ti.path,
		})
	}
	for _, gi := range r.guards {
		rep.Guards = append(rep.Guards, jsonGuard{
			Function: gi.key.String(),
			Class:    string(gi.class),
			Cuts:     gi.cuts,
			Reason:   gi.reason,
		})
	}
	for _, row := range r.layout {
		rep.Layout = append(rep.Layout, jsonLayout{
			Callee:     row.callee.String(),
			RuleArg:    row.ruleArg,
			Params:     row.params,
			Resolved:   row.resolved,
			DeclType:   row.declType,
			Note:       row.note,
			Suspicious: row.suspicious,
		})
	}

	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
