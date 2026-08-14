# Auditing an AI-Written Codebase — Experiment

A self-contained experiment for the blog post
**"Auditing an AI-Written Codebase: A Field Guide"**.

## Run it

```bash
go run audit.go
```

That's it. The program generates a sample "AI-written" Go codebase in `./sample/`
(with 5 deliberate issues baked in), then audits it and prints a report.

## What it audits

| # | Check | What it catches |
|---|-------|-----------------|
| 1 | Unused dependencies | A module required in `go.mod` but never imported |
| 2 | Unused imports | An import whose package name never appears in the file |
| 3 | Shadowed names | A variable redeclared in an inner scope, hiding the outer one |
| 4 | Duplicated logic | Two functions with identical normalized bodies (copy-paste) |
| 5 | Dead code | A package-level func/var declared but never referenced |

## Expected findings

The sample codebase contains exactly these issues, so the report should show:

1. **UNUSED DEP:** `"github.com/pkg/errors"` — required in go.mod, never imported
2. **UNUSED IMPORT:** `"os"` in main.go
3. **SHADOWED:** `"name"` redeclared in an inner scope (in `buildOrder`)
4. **DUPLICATED:** `applyDiscount()` and `applyTax()` have identical logic
5. **DEAD:** `func deadCode` is declared but never referenced

## Files

- `audit.go` — the audit tool (self-contained; generates the sample)
- `sample/go.mod` — module with one unused dependency
- `sample/main.go` — entry point with an unused import
- `sample/service.go` — shadowed var, duplicated logic, dead code

## Notes

- The real-world commands behind checks 1–2 are `go mod tidy -diff` and
  `go vet`; this experiment reimplements them offline so it runs anywhere.
- The duplication detector normalizes identifiers/literals/operators, so it
  catches copy-paste even when variable names differ.