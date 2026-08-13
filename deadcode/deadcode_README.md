# Removing Dead Code with the Official `deadcode` Tool

A hands-on demo of the Go team's whole-program dead-code tool,
[`golang.org/x/tools/cmd/deadcode`](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode),
using the small program in `main.go`.

## The demo program

`main.go` is a tiny program with one problem: the unexported function
`legacyHelper` is never called. The compiler will not complain about it —
it's simply dead weight.

```go
package main

import (
	"fmt"
	"strings"
)

func legacyHelper(s string) string {
	return strings.ToUpper(s)
}

func process(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(process("world"))
}
```

## Run the tool

`deadcode` loads the program, builds a call graph from its entry points
(`main`, `init`, tests) using Rapid Type Analysis (RTA), and reports every
function that cannot be reached from them:

```bash
go run golang.org/x/tools/cmd/deadcode@latest .
```

Expected output:

```
main.go:9:6: unreachable func: legacyHelper
```

## Remove the dead function

`legacyHelper` is only referenced by itself, so it's safe to delete — along
with the `strings` import, which nothing else uses:

```diff
 import (
 	"fmt"
-	"strings"
 )
 
-func legacyHelper(s string) string {
-	return strings.ToUpper(s)
-}
-
 func process(name string) string {
 	return fmt.Sprintf("hello, %s", name)
 }
```

The program now contains only reachable code:

```go
package main

import "fmt"

func process(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(process("world"))
}
```

## Verify

Run the tool again — no output means every remaining function is reachable:

```bash
go run golang.org/x/tools/cmd/deadcode@latest .
```

## Why the compiler misses it

The Go compiler flags compile-time errors like unused imports and unused
local variables, but it has no notion of an *unused function*. That requires
a whole-program call-graph analysis: `legacyHelper` looks perfectly fine in
isolation, and only an RTA pass can prove that nothing can ever reach it.

## Useful flags

- `-test` — include test executables as entry points. An exported function
  reported as dead under `-test` is a likely gap in test coverage.
- `-json` — machine-readable output.
- `-whylive=function` — print the shortest call path from `main` to a
  function, to understand why it is *not* dead.
- `-f='{{range .Funcs}}{{println .Position}}{{end}}'` — line-oriented output
  (useful when combining runs across GOOS/GOARCH/tags).

Note that the tool exits 0 even when it reports dead code (it only exits
non-zero on errors), so rely on the *output* — not the exit code — to tell
whether dead code remains.
