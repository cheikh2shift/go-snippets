package main

// grammar.go builds the grammar in Go instead of shipping a hand-written
// .ebnf file.
//
// WHY: four runs in a row died because grammar.ebnf was not valid EBNF.
// The parser reported "invalid char literal" / "literal not terminated"
// and then produced 325 cascading errors, so every failure looked like a
// Go bug when it was an input bug.
//
// Now the same code that consumes the grammar also produces it, and if
// the range operator cannot be spelled at all it falls back to a variant
// that needs no range operator and no char literal.

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/exp/ebnf"
)

type rangeSpelling struct {
	name string
	op   string
	text string
}

// Verified on this machine: only the Unicode ellipsis with double-quoted
// tokens parses. Single-quoted char literals are rejected outright
// ("expected term, found Char 'a'"), which is why a double quote cannot
// be written as a char literal anywhere in a grammar file.
var rangeSpellings = []rangeSpelling{
	{"unicode-ellipsis-dq", " \u2026 ", `Probe = "a" ` + "\u2026" + ` "z" .`},
	{"ascii-dots3-dq", " ... ", `Probe = "a" ... "z" .`},
	{"ascii-dots2-dq", " .. ", `Probe = "a" .. "z" .`},
	{"dash-dq", " - ", `Probe = "a" - "z" .`},
	{"unicode-ellipsis-sq", " \u2026 ", `Probe = 'a' ` + "\u2026" + ` 'z' .`},
	{"ascii-dots3-sq", " ... ", `Probe = 'a' ... 'z' .`},
	{"ascii-dots2-sq", " .. ", `Probe = 'a' .. 'z' .`},
}

// probeRanges tries each spelling on its own minimal grammar, so no
// candidate can be a no-op.
func probeRanges() (string, string, bool) {
	for _, s := range rangeSpellings {
		g, err := ebnf.Parse("probe.ebnf", strings.NewReader(s.text))
		if err != nil {
			fmt.Printf("  rejected      %-22s %s\n", s.name, s.text)
			fmt.Printf("                %v\n", err)
			continue
		}
		if hasRange(g) {
			fmt.Printf("  parsed+Range  %-22s %s\n", s.name, s.text)
			return s.name, s.op, true
		}
		fmt.Printf("  parsed, but no Range node  %-22s %s\n", s.name, s.text)
	}
	return "", "", false
}

func hasRange(g ebnf.Grammar) bool {
	for _, p := range g {
		if exprHasRange(productionExpr(p)) {
			return true
		}
	}
	return false
}

func exprHasRange(e ebnf.Expression) bool {
	if e == nil {
		return false
	}
	switch kindOf(e) {
	case "Range":
		return true
	case "Alternative", "Sequence":
		for _, x := range elementsOf(e) {
			if exprHasRange(x) {
				return true
			}
		}
	case "Group", "Option", "Repetition":
		return exprHasRange(bodyOf(e))
	}
	return false
}

// grammarText writes the grammar.
//
// Case matters: uppercase names are non-terminal productions, so white
// space is allowed between their tokens. Lowercase names are lexical, so
// their characters must match exactly. That is what keeps spaces legal
// between arguments but illegal inside a string or an identifier.
func grammarText(op string, useRanges bool) string {
	rng := func(lo, hi string) string {
		if useRanges {
			return `"` + lo + `"` + op + `"` + hi + `"`
		}
		var b strings.Builder
		for r := rune(lo[0]); r <= rune(hi[0]); r++ {
			if b.Len() > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(`"` + string(r) + `"`)
		}
		return b.String()
	}

	var sb strings.Builder
	w := func(format string, a ...any) {
		fmt.Fprintf(&sb, format, a...)
		sb.WriteString("\n")
	}

	w(`Call      = ident "(" [ ArgList ] ")" .`)
	w(`ArgList   = Arg { "," Arg } .`)
	w(`Arg       = ident "=" Value .`)
	w(`Value     = str | num | bool | List | Obj | ident .`)
	w(`List      = "[" [ Value { "," Value } ] "]" .`)
	w(`Obj       = "{" [ Field { "," Field } ] "}" .`)
	w(`Field     = ident ":" Value .`)
	w(`bool      = "true" | "false" .`)
	w(`ident     = letter { letter | digit | "_" } .`)
	w(`str       = dquote { strchar } dquote .`)
	w(`strchar   = letter | digit | space | punct | escape .`)
	w(`escape    = backslash ( dquote | backslash | "n" | "t" ) .`)
	w(`num       = [ "-" ] digit { digit } .`)
	w(`letter    = %s | %s | "_" .`, rng("a", "z"), rng("A", "Z"))
	w(`digit     = %s .`, rng("0", "9"))
	w(`space     = " " | "\t" .`)
	w(`punct     = "!" | "#" | "$" | "%%" | "&" | "'" | "(" | ")" | "*" | "+" | "," | "-" | "." | "/" | ":" | ";" | "<" | "=" | ">" | "?" | "@" | "[" | "]" | "^" | "_" | "{" | "|" | "}" | "~" .`)
	w(`dquote    = "\"" .`)
	w(`backslash = "\\" .`)

	return sb.String()
}

// buildGrammar returns the grammar, the variant that loaded, the range
// spelling that worked, and the text that was written to disk.
func buildGrammar() (ebnf.Grammar, string, string, string) {
	name, op, ok := probeRanges()

	variant, useRanges := "ranges", true
	if !ok {
		variant, useRanges, op = "portable", false, ""
	}

	text := grammarText(op, useRanges)
	g, err := ebnf.Parse("grammar.generated.ebnf", strings.NewReader(text))
	if err != nil {
		fmt.Printf("  %s variant did not parse (%v); falling back to portable\n", variant, err)
		variant, useRanges = "portable", false
		text = grammarText("", false)
		g, err = ebnf.Parse("grammar.generated.ebnf", strings.NewReader(text))
		if err != nil {
			fmt.Printf("  FATAL: the generated grammar does not parse: %v\n", err)
			os.Exit(1)
		}
	}

	_ = os.WriteFile("grammar.generated.ebnf", []byte(text), 0o644)
	return g, variant, name, text
}
