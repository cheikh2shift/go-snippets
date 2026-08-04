# OpenRouter Model Benchmark Tool

A Go CLI tool that benchmarks LLM models on OpenRouter across multiple capability
suites with fully deterministic grading — no LLM-as-judge, zero external dependencies.

Inspired by modern evaluation practice: Berkeley Function Calling Leaderboard (BFCL),
τ²-Bench, IFEval, and ClawBench (pass@k + reliability + cost-per-correct metrics).

## Features

- **Live pricing** — costs are computed from OpenRouter's `/api/v1/models` pricing, fetched at startup
- **`-list-models`** — print the full catalog with context length, `$/1M` input/output, and tool-calling support
- **4 benchmark suites** (all deterministic):
  - `accuracy` — factual QA + math, graded by exact/numeric/multi-choice match
  - `instruction` — IFEval-style constraint checks (sentence counts, keyword in/exclusion, formats, lengths)
  - `tools` — BFCL-style function calling: correct tool selection, arg values, parallel calls, restraint (when *not* to call)
  - `json` — structured output validated against a JSON schema (types, enums, required keys, ranges)
- **pass@k + reliability** — each task runs N trials; score = tasks passing at least once, plus trial pass-rate and stddev-friendly p50/p95 latency
- **Efficiency metrics** — cost per correct answer, tokens/sec, latency percentiles
- **Reports** — `report.json` (full detail), `results.csv` (flat per-run), `report.md` (summary table)

## Installation

```bash
go install github.com/cheikh2shift/go-snippets/llm-bench@latest
```

This builds the `llm-bench` binary into your `GOBIN` (usually `~/go/bin`).
Alternatively, run directly from source without installing:

```bash
go run github.com/cheikh2shift/go-snippets/llm-bench@latest -list-models
```

## Usage

```bash
# See the catalog and live pricing first (no key needed for listing)
llm-bench -list-models

# Benchmark a few models (slugs are matched exactly against the catalog)
export OPENROUTER_API_KEY=sk-or-v1-...
llm-bench -models "anthropic/claude-sonnet-5,openai/gpt-5.6-sol,deepseek/deepseek-v4-flash"

# Quick smoke run: 1 run, 2 tasks per suite
llm-bench -models "anthropic/claude-sonnet-5" -runs 1 -limit 2

# Only the tool-calling suite, 5 trials per task
llm-bench -models "qwen/qwen3-235b-a22b" -suites tools -runs 5
```

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `-token` | — | OpenRouter API key (or set `OPENROUTER_API_KEY`) |
| `-models` | — | Comma-separated OpenRouter model slugs |
| `-suites` | `accuracy,instruction,tools,json` | Suites to run |
| `-runs` | `3` | Trials per task (drives pass@k and reliability) |
| `-limit` | `0` | Max tasks per suite (0 = all) |
| `-max-tokens` | `1024` | Max completion tokens per request |
| `-temp` | `0` | Sampling temperature |
| `-retries` | `3` | Retries on HTTP 429 (rate limit) |
| `-retry-delay` | `15` | Seconds to wait between rate-limit retries |
| `-out` | `benchmark-results` | Output directory |
| `-list-models` | `false` | Print catalog + pricing and exit |

## Scoring

Per suite, each task is run `-runs` times:

- **Score (pass@k)**: % of tasks that passed in *at least one* trial
- **Reliability**: % of all trials that passed
- **Overall**: mean of the active suite scores

All grading is deterministic (exact-match normalization, numeric tolerance, regex/JSON
checks, AST-style tool-arg comparison), so results are reproducible and cost $0 to regrade.

## Tests

Deterministic graders are covered by unit tests (run without a network/API key):

```bash
go test ./...
```

## Cost Estimation

Prices are pulled live from `GET /api/v1/models`. If the catalog is unreachable the tool
falls back to $1/M input / $3/M output. Costs use per-request token usage from the API.

## License

TBA
