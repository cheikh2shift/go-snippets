# go doc -ex: The Flag That Turns Your Examples Into a Living Test Suite

An experiment to demonstrate how `go doc -ex` surfaces the examples that
`go test` already runs — turning your documentation into a living test suite.

## What this experiment shows

`go doc` normally shows only the **signatures** and **doc comments** of a
package. But Go has a hidden superpower: **example functions** (`func ExampleXxx()`)
are *both* documentation *and* tests. The `go test` runner executes them, and
`go doc -ex` displays them.

This experiment has three parts:

1. **`calc/`** — a tiny package with doc comments + 3 example functions.
2. **`run.sh`** — runs `go test` (proving the examples execute) and then
   `go doc -ex` (proving the examples are rendered as docs).
3. **`compare.sh`** — shows the *before/after*: `go doc` vs `go doc -ex`.

## How to run

```bash
cd calc
go test -v ./...        # 1. prove the examples run as tests
cd ..
./run.sh                # 2. prove go doc -ex renders them
./compare.sh            # 3. see the before/after diff
```

## The punchline

- `go test` runs your examples → they are **tests**.
- `go doc -ex` shows them → they are **documentation**.
- One function, two jobs. That's the "living test suite."
