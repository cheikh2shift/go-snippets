package main

// samples.go is the 20 calls. Eleven are valid, nine are broken.
//
// These are written as raw string literals, so \n and \" are two
// characters each, exactly as they would arrive from a model.

type sample struct {
	n     int
	call  string
	valid bool
	note  string
}

var samples = []sample{
	{1, `noop()`, true, "no arguments at all"},
	{2, `list_dir(path="/tmp")`, true, "one string argument"},
	{3, `read_file(path="/etc/hosts", mode="r")`, true, "two arguments, space after the comma"},
	{4, `search(q="title:\"Go\" and tags:ai")`, true, "escaped quotes and spaces inside a string"},
	{5, `add(a=1, b=2)`, true, "bare numbers"},
	{6, `set_flag(name="verbose", value=true)`, true, "a boolean"},
	{7, `fetch(url="https://go.dev", retries=3, headers={accept:"text/html"})`, true, "a nested object"},
	{8, `score(items=[1, 2, 3], tag="alpha")`, true, "a list with spaces after the commas"},
	{9, `write_file(path="/tmp/out.txt", body="line1\nline2")`, true, "an escape sequence inside a string"},
	{10, `ping(host="127.0.0.1", port=8080)`, true, "dots inside a string, digits outside"},
	{11, `delete_all(confirm=false)`, true, "a false boolean"},

	{12, `read_file(path="/etc/hosts", mode="r",)`, false, "trailing comma"},
	{13, `write_file(path="/tmp/x" body="hi")`, false, "missing comma"},
	{14, `search(q="unterminated)`, false, "unterminated string"},
	{15, `add(a=1, 2)`, false, "a bare value with no name"},
	{16, `list_dir(path=/tmp)`, false, "an unquoted path"},
	{17, `set_flag(name="verbose", value=)`, false, "an empty value"},
	{18, `fetch(url="https://go.dev", retries=3`, false, "no closing paren"},
	{19, `score(items=[1, 2, 3), tag="alpha")`, false, "mismatched brackets"},
	{20, `read_file(path="/etc/passwd")extra`, false, "trailing text"},
}
