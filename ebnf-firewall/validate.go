package main

// validate.go is the interpreter: it walks the ebnf AST against the raw
// rune stream of a tool call.
//
// THE FIX IN THIS FILE
//
// The ebnf package documents that "production names starting with an
// uppercase Unicode letter denote non-terminal productions (i.e.,
// productions which allow white-space and comments between tokens); all
// other production names denote lexical productions."
//
// The previous version of this file ignored that rule, so every call
// with a space after a comma was refused at the space. That is why all
// seven wrong answers pointed at a " " and wanted a letter.

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/exp/ebnf"
)

// Verdict is the result of matching one call against the grammar.
type Verdict struct {
	OK   bool
	Pos  int    // 0-based rune index of the farthest failure
	Want string // what the grammar wanted at Pos
	Prod string // the production that was being matched
}

type matcher struct {
	src  []rune
	g    ebnf.Grammar
	far  int
	want string
	prod string
}

// Validate matches input against the production named start.
func Validate(g ebnf.Grammar, start, input string) Verdict {
	m := &matcher{src: []rune(input), g: g, far: -1}
	pos, ok := m.matchName(start, 0)
	if ok && pos == len(m.src) {
		return Verdict{OK: true}
	}
	if ok {
		// matched a prefix, but not the whole input
		m.note(pos, "end of input", start)
	}
	if m.far < 0 {
		m.far, m.want, m.prod = 0, "a valid call", start
	}
	return Verdict{OK: false, Pos: m.far, Want: m.want, Prod: m.prod}
}

// note keeps the farthest failure, which is the one worth reporting.
func (m *matcher) note(pos int, want, prod string) {
	if pos > m.far {
		m.far, m.want, m.prod = pos, want, prod
	}
}

// skipWS skips white space. Only non-terminal productions may do this.
func (m *matcher) skipWS(pos int) int {
	for pos < len(m.src) {
		switch m.src[pos] {
		case ' ', '\t', '\n', '\r':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func isNonTerminal(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func (m *matcher) matchName(name string, pos int) (int, bool) {
	p, ok := m.g[name]
	if !ok {
		m.note(pos, "production "+name, name)
		return pos, false
	}
	expr := productionExpr(p)
	if expr == nil {
		m.note(pos, "the body of "+name, name)
		return pos, false
	}
	return m.match(expr, pos, isNonTerminal(name), name)
}

func (m *matcher) match(e ebnf.Expression, pos int, ws bool, prod string) (int, bool) {
	switch kindOf(e) {
	case "Alternative":
		for _, alt := range elementsOf(e) {
			if p, ok := m.match(alt, pos, ws, prod); ok {
				return p, true
			}
		}
		return pos, false

	case "Sequence":
		cur := pos
		for _, el := range elementsOf(e) {
			p, ok := m.match(el, cur, ws, prod)
			if !ok {
				return pos, false
			}
			cur = p
		}
		return cur, true

	case "Name":
		if ws {
			pos = m.skipWS(pos)
		}
		return m.matchName(stringOf(e), pos)

	case "Token":
		if ws {
			pos = m.skipWS(pos)
		}
		return m.matchToken(stringOf(e), pos, prod)

	case "Range":
		lo, hi, ok := rangeOf(e)
		if !ok {
			m.note(pos, "a character range", prod)
			return pos, false
		}
		if ws {
			pos = m.skipWS(pos)
		}
		return m.matchRange(lo, hi, pos, prod)

	case "Group":
		return m.match(bodyOf(e), pos, ws, prod)

	case "Option":
		if p, ok := m.match(bodyOf(e), pos, ws, prod); ok {
			return p, true
		}
		return pos, true

	case "Repetition":
		cur := pos
		for {
			p, ok := m.match(bodyOf(e), cur, ws, prod)
			if !ok || p == cur {
				break
			}
			cur = p
		}
		return cur, true

	case "Bad":
		m.note(pos, "a valid production", prod)
		return pos, false
	}
	m.note(pos, "a known node type", prod)
	return pos, false
}

func (m *matcher) matchToken(s string, pos int, prod string) (int, bool) {
	want := []rune(s)
	for i, r := range want {
		if pos+i >= len(m.src) || m.src[pos+i] != r {
			m.note(pos, quoteToken(s), prod)
			return pos, false
		}
	}
	return pos + len(want), true
}

func (m *matcher) matchRange(lo, hi string, pos int, prod string) (int, bool) {
	if pos >= len(m.src) {
		m.note(pos, rangeDesc(lo, hi), prod)
		return pos, false
	}
	l, h := firstRune(lo), firstRune(hi)
	r := m.src[pos]
	if r < l || r > h {
		m.note(pos, rangeDesc(lo, hi), prod)
		return pos, false
	}
	return pos + 1, true
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func quoteToken(s string) string {
	return fmt.Sprintf("%q", s)
}

func rangeDesc(lo, hi string) string {
	return fmt.Sprintf("a character in %q..%q", lo, hi)
}

func isLetterRange(want string) bool {
	return strings.Contains(want, `"a".."z"`)
}
