# leakcheck

A taint analyser for Go, built on `golang.org/x/tools/go/ssa`.

It answers one question: **does attacker-controlled data reach a dangerous
argument position?** It answers it by reading SSA, not source text, so it sees
through helper functions, string concatenation, and interface dispatch.

## Why it exists

Most Go codebases check tool input with a regex, or with a function whose name
starts with `Sanitize`. Both fail silently. This tool reads the actual SSA body
of every guard-shaped function and decides, from the instructions, whether the
value is really neutralised.

## Layout

| File | Role |
|---|---|
| `specs.go` | Pure data. Sources, sinks, propagators, sanitizers, guard-name rules. No `ssa` import. |
| `load.go` | Package loading, SSA construction, callee identity, declared-signature index. |
| `guards.go` | The guard classifier. Eight classes; three cut taint. |
| `analyze.go` | The fixpoint walk, the rule engine, the interprocedural summaries. |
| `probe.go` | Measures every sink rule index against the callee's declared signature. |
| `report.go` | Text report and JSON. |
| `main.go` | Flags, timing, the two passes. |
| `corpus/` | 19 handlers, 13 guards, 5 helpers. The test subject. |

## Run

```bash
bash run.sh
```

```powershell
.\run.ps1
```

## The two passes

Pass 1 trusts guard bodies. Pass 2 is byte-identical except `guardsNoop = true`,
so every project guard is treated as a no-op. The difference is the set of flows
that are silent **only because someone wrote a reassuring function name**.

```
SECOND PASS — every project guard treated as a no-op
  findings with guards            N
  findings without guards         M
  flows that depend on a guard    M-N
```

## The guard classes

| Class | Cuts taint | Meaning |
|---|---|---|
| `effective` | yes | delegates to a recognised sanitizer |
| `constant` | yes | takes no input |
| `non-forwarding` | yes | returns only error/bool |
| `passthrough` | no | returns its argument |
| `rewrites` | no | transforms, no recognised escaper |
| `conditional` | no | branches and transforms |
| `unknown` | no | no rule matched |

## Planted findings

| ID | Function | Expected | Why |
|---|---|---|---|
| FN-1 | `SanitizeSQLLike` | silent | delegates to `url.QueryEscape` — a recognised escaper, and the wrong one |
| FP-1 | `FilterQuery` | reported | quote-doubling is defensible; no rule exists for it |

Plus two documented false positives: `H15FixedCmd` (variadic `-1` rule) and
`H14CleanGuard` (`filepath.Base` is a propagator, not a sanitizer).

## Known limitations

1. Sanitizers are not checked against sink kind. `url.QueryEscape` stops taint at
   a SQL sink. This is FN-1.
2. Variadic sinks use `arg == -1`, so every argument is dangerous.
3. Predicate guards are not correlated with sinks. `ValidatePath(name)` protects
   only the branch that consults it.
4. Interprocedural summaries are one bit per function.
5. Only analysed packages are summarised; a path through a dependency is
   invisible unless the dependency is in the load patterns.

## Flags

| Flag | Default | Effect |
|---|---|---|
| `-http-source` | `true` | treat `net/http` accessors as taint sources |
| `-json` | `""` | write a JSON report to this path |
