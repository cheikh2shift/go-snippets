package main

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/exp/ebnf"
)

func main() {
	fmt.Println("=== E0: what the package says about its own syntax ===")
	showDoc()

	fmt.Println()
	fmt.Println("=== E1: which range spelling does this Go version accept? ===")
	g, variant, rangeOp, _ := buildGrammar()

	fmt.Println()
	fmt.Println("=== E2: which grammar text loads, and what is in it? ===")
	fmt.Printf("  variant     : %s\n", variant)
	fmt.Printf("  range op    : %s\n", rangeOp)
	fmt.Printf("  productions : %d\n", len(g))
	dumpAPI(g)
	if err := ebnf.Verify(g, "Call"); err != nil {
		fmt.Printf("  Verify(Call): FAILED: %v\n", err)
	} else {
		fmt.Printf("  Verify(Call): ok\n")
	}
	if v := Validate(g, "Call", "noop()"); v.OK {
		fmt.Printf("  smoke test  : noop() -> accepted\n")
	} else {
		fmt.Printf("  smoke test  : noop() -> REFUSED - the interpreter is broken, not the grammar\n")
		fmt.Print(diagnostic("noop()", v))
	}

	fmt.Println()
	report(g, variant, rangeOp)
}

// showDoc lets the package state its own dialect instead of us assuming it.
func showDoc() {
	out, err := exec.Command("go", "doc", "golang.org/x/exp/ebnf").CombinedOutput()
	if err != nil {
		fmt.Printf("  (go doc failed: %v)\n", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
}
