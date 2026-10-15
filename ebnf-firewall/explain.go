package main

// explain.go turns a Verdict into a sentence a human can act on.
// Every branch is keyed on what the grammar wanted and what was actually
// there, so the message is never empty.

import (
	"fmt"
	"unicode"
)

func explain(input string, v Verdict) string {
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
	eof := at >= len(src)
	var ch rune
	if !eof {
		ch = src[at]
	}

	switch {
	case v.Want == "end of input":
		return "there is trailing text after the call closes, and no production allows anything after the closing paren"

	case eof && v.Want == `"\""`:
		return "the string literal never closes; the ) your eye reads as the end of the call is inside the string"

	case eof:
		return fmt.Sprintf("the input ends here, but the grammar still needs %s", v.Want)

	case ch == ')' && isLetterRange(v.Want) && prevNonSpace(src, at) == ',':
		return "the argument list ends with a comma, and no production allows an argument list to end in a comma"

	case ch == ')' && isLetterRange(v.Want):
		return "the value is missing: every value production needs at least one character"

	case unicode.IsDigit(ch) && isLetterRange(v.Want):
		return "every argument in this grammar is name=value, and this argument is a bare value"

	case ch == '/' && v.Want == `"\""`:
		return "the value is not quoted, and a path starting with / is not an identifier"

	case ch == ')' && v.Want == `"\""`:
		return "the value is missing: every value production needs at least one character"

	case ch == ')' && v.Want == `","` && unclosed(src, at, '[', ']'):
		return "the list opened with [ and closed with ), and the grammar requires them to match"

	case ch == ')' && v.Want == `","`:
		return "the call closes here, so the grammar is looking for the end of the argument list, not another separator"

	case v.Want == `","`:
		return "the two arguments are not separated by a comma, so the grammar cannot tell where the first one ends"

	case ch == ')' && v.Want == `"]"`:
		return "the list opened with [ and closed with ), and the grammar requires them to match"
	}

	return fmt.Sprintf("the grammar expected %s here, but found %s", v.Want, describeChar(ch))
}

func prevNonSpace(src []rune, at int) rune {
	for i := at - 1; i >= 0; i-- {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return src[i]
		}
	}
	return 0
}

func unclosed(src []rune, at int, open, close rune) bool {
	depth := 0
	for i := 0; i < at && i < len(src); i++ {
		switch src[i] {
		case open:
			depth++
		case close:
			depth--
		}
	}
	return depth > 0
}

func describeChar(r rune) string {
	if r == 0 {
		return "the end of the input"
	}
	return fmt.Sprintf("%q", string(r))
}
