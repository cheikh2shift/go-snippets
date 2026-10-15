package main

// naive.go is the two baselines people actually ship.
//
// Baseline 1 has no error channel at all: it answers yes or no. That is
// the point - when it is wrong, nobody hears about it.
//
// Baseline 2 adds json.Unmarshal on each value, so it does raise errors.
// It is loud, but it is loud about valid calls, because tool-call syntax
// is not JSON: add(a=1, b=2) has an unquoted key.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var callShape = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\((.*)\)$`)

// regexValidate: shape check plus "every argument contains an =".
func regexValidate(input string) (bool, error) {
	m := callShape.FindStringSubmatch(input)
	if m == nil {
		return false, nil
	}
	body := m[2]
	if body == "" {
		return true, nil
	}
	for _, a := range splitArgs(body) {
		if !strings.Contains(a, "=") {
			return false, nil
		}
	}
	return true, nil
}

// jsonValidate: same shape check, then json.Unmarshal on every value.
func jsonValidate(input string) (bool, error) {
	m := callShape.FindStringSubmatch(input)
	if m == nil {
		return false, fmt.Errorf("does not look like a call")
	}
	body := m[2]
	if body == "" {
		return true, nil
	}
	for _, a := range splitArgs(body) {
		i := strings.Index(a, "=")
		if i < 0 {
			return false, fmt.Errorf("argument %q has no =", a)
		}
		name := strings.TrimSpace(a[:i])
		val := strings.TrimSpace(a[i+1:])
		if name == "" {
			return false, fmt.Errorf("argument %q has no name", a)
		}
		if val == "" {
			return false, fmt.Errorf("argument %q has no value", a)
		}
		var v any
		if err := json.Unmarshal([]byte(val), &v); err != nil {
			return false, fmt.Errorf("value of %s is not JSON: %v", name, err)
		}
	}
	return true, nil
}

// splitArgs splits on commas at depth zero, ignoring commas inside
// quotes and inside [], {} and (). This is the careful version, and it
// is still wrong: it cannot see that "path=/tmp/x" body="hi" is two
// arguments, because nothing separates them.
func splitArgs(s string) []string {
	var out []string
	depth := 0
	inStr := false
	esc := false
	start := 0
	for i, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\' && inStr:
			esc = true
		case r == '"':
			inStr = !inStr
		case inStr:
			// inside a string: commas and brackets do not count
		case r == '[' || r == '{' || r == '(':
			depth++
		case r == ']' || r == '}' || r == ')':
			depth--
		case r == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}
