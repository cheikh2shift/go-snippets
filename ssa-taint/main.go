package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

func main() {
	httpSource := flag.Bool("http-source", true,
		"treat net/http request accessors as taint sources")
	jsonPath := flag.String("json", "",
		"write a JSON report to this path")
	flag.Parse()

	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	httpSourceOn = *httpSource

	var res runResult

	t := time.Now()
	p, err := loadProgram(patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
	res.loadDur = time.Since(t)

	t = time.Now()
	classifyGuards(p)
	res.guardDur = time.Since(t)

	t = time.Now()
	computeSummaries(p)
	res.summaryDur = time.Since(t)

	// Pass 1: guards trusted.
	guardsNoop = false
	t = time.Now()
	a1 := newAnalyzer(p)
	for _, fn := range p.funcs {
		a1.run(fn, false, true)
	}
	res.pass1Dur = time.Since(t)
	res.findings = a1.findings

	// Pass 2: byte-identical except that every project guard is a no-op.
	guardsNoop = true
	t = time.Now()
	a2 := newAnalyzer(p)
	for _, fn := range p.funcs {
		a2.run(fn, false, true)
	}
	res.pass2Dur = time.Since(t)
	res.second = a2.findings
	guardsNoop = false

	t = time.Now()
	res.layout = probeLayout(p)
	res.probeDur = time.Since(t)

	for _, gi := range p.guards {
		res.guards = append(res.guards, gi)
	}
	sort.Slice(res.guards, func(i, j int) bool {
		return res.guards[i].key.String() < res.guards[j].key.String()
	})
	sort.Slice(res.findings, func(i, j int) bool {
		ri, rj := severityRank[res.findings[i].severity()], severityRank[res.findings[j].severity()]
		if ri != rj {
			return ri < rj
		}
		return res.findings[i].fn.String() < res.findings[j].fn.String()
	})

	printReport(os.Stdout, p, &res)

	if *jsonPath != "" {
		if err := writeJSON(*jsonPath, p, &res); err != nil {
			fmt.Fprintln(os.Stderr, "json:", err)
			os.Exit(1)
		}
		fmt.Printf("JSON report written to %s\n", *jsonPath)
	}
}
