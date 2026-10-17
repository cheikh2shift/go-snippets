package main

import (
	"regexp"
	"strings"
)

// key identifies a callable:
//
//	pkg  import path
//	recv base name of the receiver type; "" for package-level functions,
//	     or the interface name for dynamic (invoke) calls
//	name bare function or method name
type key struct {
	pkg  string
	recv string
	name string
}

func (k key) String() string {
	if k.recv != "" {
		return k.pkg + "." + k.recv + "." + k.name
	}
	return k.pkg + "." + k.name
}

// shortName drops the import path, keeping the last element of the package.
func shortName(k key) string {
	base := k.pkg
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case base == "" && k.recv == "":
		return k.name
	case k.recv != "":
		return base + "." + k.recv + "." + k.name
	default:
		return base + "." + k.name
	}
}

// ---------------------------------------------------------------- specs

type srcSpec struct {
	key   key
	label string
}

// sinkSpec names one dangerous argument position.
//
// Argument indices are absolute positions in ssa.CallCommon.Args. For a
// concrete method call that slice includes the receiver, so Header.Set has the
// taintable value at index 2 while os.Open has the path at index 0. The tool
// does not take that layout on faith: probe.go records the declared parameter
// type sitting at each matched index and prints it in the report.
//
// arg == -1 means "any argument" — the only honest option for variadic calls
// like exec.Command(name, args...), where the dangerous position is not fixed.
type sinkSpec struct {
	key  key
	arg  int
	kind string
}

// ---------------------------------------------------------------- sources

// Where untrusted data enters the program. Every entry is a *return* value, so
// a match means "this call creates tainted data", not "this call is dangerous".
var sourceSpecs = []srcSpec{
	{key{"net/http", "Request", "FormValue"}, "request.form"},
	{key{"net/http", "Request", "PostFormValue"}, "request.form"},
	{key{"net/http", "Request", "Cookie"}, "request.cookie"},
	{key{"net/http", "Request", "CookieValue"}, "request.cookie"},
	{key{"net/http", "Header", "Get"}, "request.header"},
	{key{"net/http", "Request", "PathValue"}, "request.path"},
	{key{"net/url", "Values", "Get"}, "request.query"},
	{key{"net/url", "URL", "Query"}, "request.query"},
	{key{"os", "", "Getenv"}, "environment"},
	{key{"os", "", "LookupEnv"}, "environment"},
	{key{"io", "", "ReadAll"}, "request.body"},
	{key{"bufio", "Scanner", "Text"}, "request.body"},
	{key{"bufio", "Reader", "ReadString"}, "request.body"},
	{key{"bufio", "Reader", "ReadBytes"}, "request.body"},
}

// ---------------------------------------------------------------- sinks

var sinkSpecs = []sinkSpec{
	// exec.Command(name, args...) — variadic, so every position counts.
	{key{"os/exec", "", "Command"}, -1, "command-injection"},
	{key{"os/exec", "", "CommandContext"}, -1, "command-injection"},

	// database/sql. Index 1 is the query string on DB; the bound parameters
	// after it are NOT dangerous, which is the entire point of parametrising.
	{key{"database/sql", "DB", "Query"}, 1, "sql-injection"},
	{key{"database/sql", "DB", "QueryContext"}, 2, "sql-injection"},
	{key{"database/sql", "DB", "Exec"}, 1, "sql-injection"},
	{key{"database/sql", "DB", "ExecContext"}, 2, "sql-injection"},

	{key{"os", "", "Open"}, 0, "path-traversal"},
	{key{"os", "", "OpenFile"}, 0, "path-traversal"},
	{key{"os", "", "Create"}, 0, "path-traversal"},
	{key{"os", "", "ReadFile"}, 0, "path-traversal"},
	{key{"os", "", "WriteFile"}, 0, "path-traversal"},

	{key{"net/http", "", "Get"}, -1, "ssrf"},
	{key{"net/http", "", "Post"}, -1, "ssrf"},
	{key{"net", "", "Dial"}, 1, "ssrf"},
	{key{"net", "", "DialTimeout"}, 2, "ssrf"},

	{key{"log", "", "Print"}, -1, "log-leak"},
	{key{"log", "", "Printf"}, -1, "log-leak"},
	{key{"log", "", "Println"}, -1, "log-leak"},

	{key{"fmt", "", "Print"}, -1, "output-sink"},
	{key{"fmt", "", "Printf"}, -1, "output-sink"},
	{key{"fmt", "", "Println"}, -1, "output-sink"},
	{key{"fmt", "", "Fprint"}, -1, "output-sink"},
	{key{"fmt", "", "Fprintf"}, -1, "output-sink"},
	{key{"fmt", "", "Fprintln"}, -1, "output-sink"},

	{key{"net/http", "Header", "Set"}, 2, "output-sink"},
	{key{"net/http", "Header", "Add"}, 2, "output-sink"},
	{key{"net/http", "ResponseWriter", "Write"}, -1, "output-sink"},
	{key{"os", "File", "Write"}, -1, "output-sink"},
	{key{"os", "", "Setenv"}, -1, "output-sink"},
}

// ---------------------------------------------------------------- propagation

// propagators forward taint from any argument to their result. This list is
// what decides how much precision the analysis has: anything missing here is a
// false negative unless it lives in project code, where the interprocedural
// summary takes over.
//
// Note what is deliberately absent. url.QueryEscape and html.EscapeString are
// sanitizers, not propagators. And no strings.* function is a sanitizer —
// trimming and lowercasing do not make input safe.
//
// The key of a map[key]bool literal is already a key value, so the entries are
// written key{...}: true — NOT {key{...}}: true. The extra braces would make
// the compiler read the outer pair as an elided key literal and treat the inner
// key{...} as the value of its first field, which is a string.
var propagators = map[key]bool{
	key{"fmt", "", "Sprintf"}:        true,
	key{"fmt", "", "Sprint"}:         true,
	key{"fmt", "", "Sprintln"}:       true,
	key{"fmt", "", "Errorf"}:         true,
	key{"strings", "", "Join"}:       true,
	key{"strings", "", "Replace"}:    true,
	key{"strings", "", "ReplaceAll"}: true,
	key{"strings", "", "ToLower"}:    true,
	key{"strings", "", "ToUpper"}:    true,
	key{"strings", "", "TrimSpace"}:  true,
	key{"strings", "", "Trim"}:       true,
	key{"strings", "", "TrimPrefix"}: true,
	key{"strings", "", "TrimSuffix"}: true,
	key{"path", "", "Join"}:          true,
	key{"path", "", "Base"}:          true,
	key{"path", "", "Dir"}:           true,
	key{"path", "", "Clean"}:         true,
	key{"path/filepath", "", "Join"}: true,
	key{"path/filepath", "", "Base"}: true,
	key{"path/filepath", "", "Dir"}:  true,
	key{"path/filepath", "", "Clean"}: true,
	key{"net/url", "URL", "Query"}:   true,
	key{"net/url", "URL", "String"}:  true,
}

// sanitizers genuinely neutralise taint *for the context they were written
// for*. A call to one of these stops the flow.
//
// This map is the tool's central bet, and the corpus is built to expose how it
// can be lost. url.QueryEscape is a correct answer to "will this break my URL?"
// and a wrong answer to "is this safe to concatenate into SQL?" — but the map
// has no notion of context, so the second question gets the first answer.
var sanitizers = map[key]string{
	key{"net/url", "", "QueryEscape"}: "net/url.QueryEscape",
	key{"net/url", "", "PathEscape"}:  "net/url.PathEscape",
	key{"html", "", "EscapeString"}:   "html.EscapeString",
	key{"strconv", "", "Quote"}:       "strconv.Quote",
	key{"regexp", "", "QuoteMeta"}:    "regexp.QuoteMeta",
}

// ---------------------------------------------------------------- guards

// A "guard" is any function in the analysed packages whose name suggests
// sanitisation. Name matching is crude on purpose: the point of the exercise is
// then to prove from the SSA body what each one actually does.
//
// The keyword must lead the name, and handlers are excluded. A handler is an
// entry point that consumes a request; it is not a transformation applied to a
// value. Without that exclusion, FilterHandler would be mistaken for a guard
// purely because it shares a prefix with FilterQuery.
var guardName = regexp.MustCompile(
	`(?i)^(is|has|try|maybe)?(sanitiz|sanitis|escap|clean|valid|strip|scrub|redact|safe|filter|neutraliz|normaliz|shield|trim)`)

func isGuardName(n string) bool {
	if strings.HasSuffix(n, "Handler") || strings.HasSuffix(n, "HandlerFunc") {
		return false
	}
	return guardName.MatchString(n)
}

// ---------------------------------------------------------------- severity

var severityOf = map[string]string{
	"command-injection": "HIGH",
	"sql-injection":     "HIGH",
	"path-traversal":    "HIGH",
	"ssrf":              "HIGH",
	"log-leak":          "MEDIUM",
	"output-sink":       "MEDIUM",
}

var severityRank = map[string]int{"HIGH": 0, "MEDIUM": 1, "LOW": 2}

// ---------------------------------------------------------------- indices

var (
	sourceIndex map[key]string
	sinkIndex   map[key][]sinkSpec
)

// ---------------------------------------------------------------- switches

// httpSourceOn is set from -http-source. When false, the net/http rows in
// sourceSpecs are skipped, which models a service that sits behind an
// authenticating proxy and never sees raw request data.
var httpSourceOn = true

// guardsNoop is set from -guards-noop. When true, every project guard is
// treated as a no-op, so taint flows straight through it. The difference
// between the two passes is the set of flows that are silent only because
// someone wrote a reassuring function name.
var guardsNoop = false

func init() {
	sourceIndex = make(map[key]string, len(sourceSpecs))
	for _, s := range sourceSpecs {
		sourceIndex[s.key] = s.label
	}
	sinkIndex = make(map[key][]sinkSpec, len(sinkSpecs))
	for _, s := range sinkSpecs {
		sinkIndex[s.key] = append(sinkIndex[s.key], s)
	}
}
