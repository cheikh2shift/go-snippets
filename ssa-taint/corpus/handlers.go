// Package corpus is the test subject. Every function here is a handler-shaped
// function that the analyser is expected to reach a specific verdict on.
//
// The labels in the comments are the *expected* verdicts. If the tool disagrees
// with a label, the label is a finding too — do not edit the corpus to make the
// tool look right.
package corpus

import (
	"database/sql"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"os/exec"
)

var db *sql.DB

// H01 — SQL injection, direct. EXPECT: sql-injection, HIGH.
func H01Query(r *http.Request) {
	q := r.FormValue("q")
	db.Query("SELECT * FROM users WHERE name = '" + q + "'")
}

// H02 — SQL injection from the environment. EXPECT: sql-injection, HIGH.
func H02EnvQuery() {
	table := os.Getenv("TABLE")
	db.Query("SELECT * FROM " + table)
}

// H03 — safe: parameterised. EXPECT: silent.
func H03SafeQuery(r *http.Request) {
	q := r.FormValue("q")
	db.Query("SELECT * FROM users WHERE name = ?", q)
}

// H04 — command injection. EXPECT: command-injection, HIGH.
func H04Exec(r *http.Request) {
	host := r.FormValue("host")
	exec.Command("ping", "-c", "1", host)
}

// H05 — path traversal. EXPECT: path-traversal, HIGH.
func H05Open(r *http.Request) {
	name := r.FormValue("file")
	os.Open("/srv/data/" + name)
}

// H06 — SSRF. EXPECT: ssrf, HIGH.
func H06Fetch(r *http.Request) {
	target := r.FormValue("url")
	http.Get(target)
}

// H07 — log leak. EXPECT: log-leak, MEDIUM.
func H07Log(r *http.Request) {
	token := r.Header.Get("Authorization")
	log.Printf("auth attempt: %s", token)
}

// H08 — output sink. EXPECT: output-sink, MEDIUM.
func H08Write(w http.ResponseWriter, r *http.Request) {
	msg := r.FormValue("msg")
	fmt.Fprintf(w, "<p>%s</p>", msg)
}

// H09 — header injection. EXPECT: output-sink, MEDIUM, arg 2.
func H09Header(w http.ResponseWriter, r *http.Request) {
	trace := r.FormValue("trace")
	w.Header().Set("X-Trace", trace)
}

// H10 — two hops through project helpers. EXPECT: path-traversal, HIGH.
func H10TwoHop(r *http.Request) {
	name := r.FormValue("name")
	os.Open(wrap2(name))
}

// H11 — one hop through a passthrough helper. EXPECT: path-traversal, HIGH.
func H11OneHop(r *http.Request) {
	name := r.FormValue("name")
	os.Open(identity(name))
}

// H12 — FN-1. SanitizeSQLLike delegates to url.QueryEscape, which the tool
// trusts unconditionally. EXPECT: silent. That silence is the planted bug.
func H12SanitizedSQL(r *http.Request) {
	q := r.FormValue("q")
	db.Query("SELECT * FROM t WHERE a = '" + SanitizeSQLLike(q) + "'")
}

// H13 — FP-1. Quote doubling is a defensible defence, but the tool has no rule
// for it. EXPECT: sql-injection, HIGH. That report is the planted false
// positive.
func H13FilteredSQL(r *http.Request) {
	q := r.FormValue("q")
	db.Query("SELECT * FROM t WHERE a = '" + FilterQuery(q) + "'")
}

// H14 — FP. filepath.Base is a propagator, not a sanitizer. EXPECT:
// path-traversal, HIGH.
func H14CleanGuard(r *http.Request) {
	name := r.FormValue("file")
	os.Open(CleanGuard(name))
}

// H15 — FP. The command is constant, but the variadic rule cannot tell.
// EXPECT: command-injection, HIGH.
func H15FixedCmd(r *http.Request) {
	name := r.FormValue("name")
	exec.Command("echo", name)
}

// H16 — documented limitation. ValidatePath is a predicate, so it protects only
// the branch that consults it. The analyser does not correlate branch values
// with sinks, so reading the raw name is still reported. EXPECT:
// path-traversal, HIGH.
func H16PathHandler(r *http.Request) {
	name := r.FormValue("file")
	if ValidatePath(name) {
		os.Open(name)
	}
}

// H17 — safe: no taint reaches a sink. EXPECT: silent.
func H17Constant() {
	os.Open("/etc/hostname")
}

// H18 — safe: sanitized for the right context. EXPECT: silent.
func H18EscapedHTML(w http.ResponseWriter, r *http.Request) {
	msg := r.FormValue("msg")
	fmt.Fprintf(w, "<p>%s</p>", html.EscapeString(msg))
}

// H19 — taint through a slice literal and a join. EXPECT: path-traversal, HIGH.
//
// This case exists because the slice instruction reads the backing array, not
// the element address that was stored to. Without the aggregate rule in
// analyze.go, the taint is lost here.
func H19Join(r *http.Request) {
	parts := []string{"a", r.FormValue("b")}
	os.Open(joinPath(parts))
}
