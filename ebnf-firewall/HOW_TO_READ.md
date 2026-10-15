# How to read the output

## What changed, and why

Four runs failed before this one, and none of them failed in the Go code.

| Run | What actually broke |
|---|---|
| 1 | `impossible type switch case` — `Pos()` has pointer receivers, so nodes are `*ebnf.Name`, not `ebnf.Name`. Also `Production` has no `Expression` field; it is `Expr`. |
| 2 | `grammar.ebnf:6:42: invalid char literal` — the hand-written grammar used `'a'` char literals, which this parser rejects. |
| 3 | `grammar.ebnf:8:12: expected "term", found Char '"'` — same cause, one line further down. |
| 4 | The grammar loaded, but **every call with a space after a comma was refused at the space.** |

Run 4 is the interesting one. The `ebnf` package documents:

> Production names starting with an uppercase Unicode letter denote
> non-terminal productions (i.e., productions which allow white-space and
> comments between tokens); all other production names denote lexical
> productions.

The interpreter ignored that rule. So `read_file(path="/etc/hosts", mode="r")`
was refused at character 28 — the space — with the grammar "wanting a
character in a..z". Seven of the twenty calls failed that way, and all
seven are valid.

The fix is one idea: **white space is skipped between tokens only inside
a production whose name starts with an uppercase letter.** That is why
the grammar now spells its non-terminals `Call`, `ArgList`, `Arg`,
`Value`, `List`, `Obj`, `Field` and its lexical productions `ident`,
`str`, `num`, `bool`, `letter`, `digit`, `space`, `punct`, `escape`,
`dquote`, `backslash`.

Consequence: spaces are legal between arguments, and illegal inside a
string or an identifier. That is exactly the behaviour you want.

## The grammar is generated, not written

`grammar.ebnf` is gone. `grammar.go` builds the text and writes
`grammar.generated.ebnf` at runtime, so you can read what actually
loaded. If the range operator cannot be spelled at all, it falls back to
a variant that enumerates `a|b|c|...` and needs no range operator and no
char literal.

Verified on this machine: only `"a" … "z"` parses. Single-quoted char
literals are rejected outright, which is why a double quote cannot be
written as a char literal anywhere in a grammar file.

## E0 / E1 / E2

- **E0** runs `go doc golang.org/x/exp/ebnf` so the package states its
  own dialect instead of us assuming it.
- **E1** tries eight range spellings, each on its own minimal grammar, so
  no candidate can be a no-op.
- **E2** prints the variant, the range operator, the production count,
  the real `Production` field list, and a census of AST node kinds.
  `Verify(Call)` then checks the grammar is well formed, and a smoke test
  matches `noop()` before the twenty calls run. If the smoke test fails,
  the interpreter is broken and the census is the ground truth.

## The three columns

| Column | What it is |
|---|---|
| **grammar** | The EBNF interpreter. No error channel: a refusal *is* the announcement. |
| **regex** | Shape check plus "every argument contains an `=`". No error channel at all. |
| **regex+JSON** | Same shape check, then `json.Unmarshal` on every value. |

`silent` means the checker was wrong and said nothing — it accepted a
broken call. `announced` means it was wrong and raised an error — it
refused a valid call.

## The numbers are this machine's

They changed once already when `samples.go` was rewritten, and they will
change again if `naive.go` changes. Re-quote them after any edit. The
test asserts invariants (the regex baseline is wrong at least once, and
silent at least once) rather than fixed counts, so it stays honest.

## Known limits

- The interpreter does not implement comments. The package allows them
  between tokens in non-terminal productions; this grammar has none.
- `Repetition` is greedy and does not backtrack. That is fine for this
  grammar and is why `read_file(path="/etc/hosts", mode="r",)` is
  refused at the trailing comma rather than somewhere else.
- The diagnostic reports the *farthest* failure, which is usually the
  most useful one but not always the one a human would pick.
- `punct` deliberately excludes `"` and `\`. Those terminate a string and
  start an escape, and they are handled by `dquote` and `escape`.
