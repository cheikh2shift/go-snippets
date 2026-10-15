package main

// report.go prints the five-part output. It reads the Verdict directly,
// so there is no regex parsing of error strings anywhere.

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/exp/ebnf"
)

var errRefused = errors.New("refused")

type tally struct {
	correct, wrong, silent, announced int
}

// add records one result.
//
// "silent" means the checker was wrong and said nothing: it accepted a
// broken call. "announced" means it was wrong and raised an error: it
// refused a valid call. The grammar has no error channel, so a refusal
// is its announcement.
func (t *tally) add(ok bool, err error, truth bool) {
	if ok == truth {
		t.correct++
		return
	}
	t.wrong++
	if err != nil {
		t.announced++
	} else {
		t.silent++
	}
}

func (t tally) String() string {
	return fmt.Sprintf("correct %2d/%d  wrong %2d  silent %2d  announced %2d",
		t.correct, len(samples), t.wrong, t.silent, t.announced)
}

func diagnostic(input string, v Verdict) string {
	if v.OK {
		return ""
	}
	src := []rune(input)
	at := v.Pos
	if at < 0 {
		at = 0
	}
	if at > len(src) {
		at = len(src)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "      %s\n", input)
	fmt.Fprintf(&b, "      %s^\n", strings.Repeat(" ", at))
	if at >= len(src) {
		fmt.Fprintf(&b, "      character %d is the end of the input\n", at)
	} else {
		fmt.Fprintf(&b, "      character %d is %q\n", at, string(src[at]))
	}
	fmt.Fprintf(&b, "      the grammar wanted  : %s\n", v.Want)
	fmt.Fprintf(&b, "      in plain English    : %s\n", explain(input, v))
	return b.String()
}

func gword(ok bool) string {
	if ok {
		return "accepted"
	}
	return "refused"
}

func bword(ok bool) string {
	if ok {
		return "ok"
	}
	return "no"
}

func report(g ebnf.Grammar, variant, rangeOp string) {
	fmt.Println("=== E3: the 20 calls ===")

	type row struct {
		s    sample
		gv   Verdict
		rOK  bool
		rErr error
		jOK  bool
		jErr error
	}

	var gt, rt, jt tally
	rows := make([]row, 0, len(samples))
	for _, s := range samples {
		gv := Validate(g, "Call", s.call)
		rOK, rErr := regexValidate(s.call)
		jOK, jErr := jsonValidate(s.call)
		rows = append(rows, row{s, gv, rOK, rErr, jOK, jErr})

		var gErr error
		if !gv.OK {
			gErr = errRefused
		}
		gt.add(gv.OK, gErr, s.valid)
		rt.add(rOK, rErr, s.valid)
		jt.add(jOK, jErr, s.valid)
	}

	fmt.Println()
	fmt.Println("=== 1. scoreboard ===")
	fmt.Printf("  grammar     %s\n", gt)
	fmt.Printf("  regex       %s\n", rt)
	fmt.Printf("  regex+JSON  %s\n", jt)

	fmt.Println()
	fmt.Println("=== 2. call by call ===")
	fmt.Printf("  %-3s %-7s %-9s %-7s %-7s %s\n", "#", "truth", "grammar", "regex", "json", "call")
	for _, r := range rows {
		truth := "valid"
		if !r.s.valid {
			truth = "broken"
		}
		mark := " "
		if r.gv.OK != r.s.valid {
			mark = "!"
		}
		fmt.Printf("  %s%-2d %-7s %-9s %-7s %-7s %s\n",
			mark, r.s.n, truth, gword(r.gv.OK), bword(r.rOK), bword(r.jOK), r.s.call)
	}

	fmt.Println()
	fmt.Println("=== 3. what the grammar said when it refused ===")
	for _, r := range rows {
		if r.gv.OK {
			continue
		}
		verdict := "correctly refused"
		if r.s.valid {
			verdict = "WRONG - this call is valid"
		}
		fmt.Printf("\n  #%d\n", r.s.n)
		fmt.Print(diagnostic(r.s.call, r.gv))
		fmt.Printf("      verdict             : %s\n", verdict)
	}

	fmt.Println()
	fmt.Println("=== 4. headline numbers ===")
	fmt.Printf("  grammar     : %d of %d correct; %d wrong\n", gt.correct, len(samples), gt.wrong)
	fmt.Printf("  regex       : %d of %d correct; %d wrong, %d of those silent\n", rt.correct, len(samples), rt.wrong, rt.silent)
	fmt.Printf("  regex+JSON  : %d of %d correct; %d wrong, %d of those silent\n", jt.correct, len(samples), jt.wrong, jt.silent)
	fmt.Println("  These come from this machine and this run.")
	fmt.Println("  Quote them with the sample set as it stands now - the numbers")
	fmt.Println("  change whenever samples.go or naive.go changes.")
}
