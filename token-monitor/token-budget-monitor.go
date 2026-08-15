package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

// ModelSpec holds the token-related specifications of an LLM that drive
// how input ("in") and output ("out") token usage is estimated and priced.
type ModelSpec struct {
	Name          string
	Vendor        string
	ContextWindow int64   // max input + output tokens per request
	MaxOutput     int64   // max output tokens the model may generate
	InputPrice    float64 // USD per 1,000,000 input tokens
	OutputPrice   float64 // USD per 1,000,000 output tokens
	CharsPerToken float64 // tokenizer heuristic: average characters per token
}

// TokenEstimate is the full usage/cost breakdown computed for one request.
type TokenEstimate struct {
	InputChars   int
	OutputChars  int
	InputTokens  int64
	RawOutput    int64   // estimate before MaxOutput clamping
	OutputTokens int64
	TotalTokens  int64
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
	ContextPct   float64
	OutputPct    float64
	Warnings     []string
}

// estimateTokens derives token usage from the IN and OUT phrases using the
// model's chars-per-token heuristic, enforces the model's specs
// (MaxOutput cap, context window) and prices the result.
func estimateTokens(s ModelSpec, inPhrase, outPhrase string) TokenEstimate {
	inChars := len(inPhrase)
	outChars := len(outPhrase)
	e := TokenEstimate{InputChars: inChars, OutputChars: outChars}

	// IN: tokenize the prompt characters
	e.InputTokens = int64(math.Ceil(float64(inChars) / s.CharsPerToken))

	// OUT: tokenize the response characters, then cap at MaxOutput
	e.RawOutput = int64(math.Ceil(float64(outChars) / s.CharsPerToken))
	e.OutputTokens = e.RawOutput
	if e.RawOutput > s.MaxOutput {
		e.OutputTokens = s.MaxOutput
		e.Warnings = append(e.Warnings, "raw output exceeds MaxOutput; capped")
	}

	e.TotalTokens = e.InputTokens + e.OutputTokens

	// Context window check (input + output must fit)
	if e.TotalTokens > s.ContextWindow {
		e.ContextPct = 100
		e.Warnings = append(e.Warnings, "total exceeds context window")
	} else {
		e.ContextPct = float64(e.TotalTokens) / float64(s.ContextWindow) * 100
	}
	if s.MaxOutput > 0 {
		e.OutputPct = float64(e.OutputTokens) / float64(s.MaxOutput) * 100
	}

	// Price with the model's per-token rates (per 1M tokens)
	e.InputCost = float64(e.InputTokens) / 1e6 * s.InputPrice
	e.OutputCost = float64(e.OutputTokens) / 1e6 * s.OutputPrice
	e.TotalCost = e.InputCost + e.OutputCost

	return e
}

// bar renders a static ASCII progress bar, e.g. [####------] 40%.
func bar(current, max float64, width int) string {
	pct := 0.0
	if max > 0 {
		pct = current / max
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

// headBar renders a download-style bar with a moving head, e.g. [===>----].
func headBar(current, max float64, width int) string {
	pct := 0.0
	if max > 0 {
		pct = current / max
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled >= width {
		return "[" + strings.Repeat("=", width) + "]"
	}
	if filled == 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}
	return "[" + strings.Repeat("=", filled-1) + ">" + strings.Repeat("-", width-filled) + "]"
}

// animateStream redraws a single line to simulate output tokens streaming in.
// The bar fills with time; the percentage shows progress toward the expected
// output token count.
func animateStream(e TokenEstimate, width int) {
	steps := 30
	for i := 0; i <= steps; i++ {
		cur := int64(float64(i) / float64(steps) * float64(e.OutputTokens))
		pct := float64(i) / float64(steps) * 100
		h := headBar(float64(i), float64(steps), width)
		fmt.Printf("\r   OUT streaming: %6d tok %s %5.1f%%   ", cur, h, pct)
		time.Sleep(25 * time.Millisecond)
	}
	fmt.Println()
}

// demoRequest runs the full calculation walk-through for one model.
func demoRequest(m ModelSpec, inPhrase, outPhrase string, width int) TokenEstimate {
	fmt.Println(strings.Repeat("=", 76))
	fmt.Printf("  %-28s (%s)\n", m.Name, m.Vendor)
	fmt.Println(strings.Repeat("=", 76))

	fmt.Printf("  Spec    : context %d | max output %d | tokenizer ~%.1f chars/token\n",
		m.ContextWindow, m.MaxOutput, m.CharsPerToken)
	fmt.Printf("  Price   : $%.3f /1M in | $%.3f /1M out\n", m.InputPrice, m.OutputPrice)

	e := estimateTokens(m, inPhrase, outPhrase)

	fmt.Println()
	fmt.Println("  Phrases:")
	fmt.Printf("    IN  : %q  (%d chars)\n", shorten(inPhrase, 64), e.InputChars)
	fmt.Printf("    OUT : %q  (%d chars)\n", shorten(outPhrase, 64), e.OutputChars)

	fmt.Println()
	fmt.Println("  Math (tokenizer heuristic):")
	fmt.Printf("    IN  : ceil(%d chars / %.1f) = %d tokens\n", e.InputChars, m.CharsPerToken, e.InputTokens)
	if e.RawOutput != e.OutputTokens {
		fmt.Printf("    OUT : ceil(%d chars / %.1f) = %d raw -> capped to %d\n",
			e.OutputChars, m.CharsPerToken, e.RawOutput, e.OutputTokens)
	} else {
		fmt.Printf("    OUT : ceil(%d chars / %.1f) = %d tokens\n", e.OutputChars, m.CharsPerToken, e.RawOutput)
	}
	fmt.Printf("    TOTAL: %d in + %d out = %d tokens\n", e.InputTokens, e.OutputTokens, e.TotalTokens)

	fmt.Println()
	fmt.Printf("  Context : %s %7.3f%% of %d\n",
		bar(float64(e.TotalTokens), float64(m.ContextWindow), width), e.ContextPct, m.ContextWindow)
	fmt.Printf("  Output  : %s %7.3f%% of %d max\n",
		bar(float64(e.OutputTokens), float64(m.MaxOutput), width), e.OutputPct, m.MaxOutput)

	fmt.Println()
	fmt.Printf("  Cost    : %d/1M * $%.3f  +  %d/1M * $%.3f  =  $%.6f + $%.6f = $%.6f\n",
		e.InputTokens, m.InputPrice, e.OutputTokens, m.OutputPrice,
		e.InputCost, e.OutputCost, e.TotalCost)

	for _, w := range e.Warnings {
		fmt.Printf("  WARNING : %s\n", w)
	}

	fmt.Println()
	fmt.Println("  Live stream (output tokens generated over time):")
	animateStream(e, width)
	fmt.Println()

	return e
}

// printSummary renders the side-by-side comparison table.
func printSummary(models []ModelSpec, estimates []TokenEstimate) {
	fmt.Println(strings.Repeat("=", 84))
	fmt.Printf("  %-18s %9s %9s %9s %9s %14s\n", "Model", "IN", "OUT", "TOTAL", "CONTEXT%", "COST")
	fmt.Println(strings.Repeat("-", 84))
	for i := range models {
		e := estimates[i]
		fmt.Printf("  %-18s %9d %9d %9d %8.3f%% %13.6f$\n",
			models[i].Name, e.InputTokens, e.OutputTokens, e.TotalTokens, e.ContextPct, e.TotalCost)
	}
	fmt.Println(strings.Repeat("=", 84))
}

// shorten truncates a string for display, appending "..." when cut.
func shorten(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// filterModels keeps only models whose name matches the comma-separated filter.
func filterModels(all []ModelSpec, filter string) []ModelSpec {
	var out []ModelSpec
	for _, n := range strings.Split(filter, ",") {
		for _, m := range all {
			if strings.EqualFold(m.Name, strings.TrimSpace(n)) {
				out = append(out, m)
			}
		}
	}
	return out
}

// allModels returns the model variations used for the demo.
func allModels() []ModelSpec {
	return []ModelSpec{
		{"GPT-4o", "OpenAI", 128000, 16384, 2.50, 10.00, 3.8},
		{"GPT-4o mini", "OpenAI", 128000, 16384, 0.15, 0.60, 3.8},
		{"GPT-4.1 nano", "OpenAI", 1048576, 32768, 0.10, 0.40, 3.8},
		{"Claude Sonnet 4", "Anthropic", 200000, 64000, 3.00, 15.00, 3.5},
		{"Claude Haiku 4", "Anthropic", 200000, 64000, 0.80, 4.00, 3.5},
		{"Gemini 1.5 Flash", "Google", 1048576, 8192, 0.075, 0.30, 4.0},
		{"Llama 3.1 405B", "Meta", 128000, 8192, 3.00, 3.00, 4.0},
		{"DeepSeek V3", "DeepSeek", 128000, 8192, 0.27, 1.10, 3.8},
		{"Mistral Large 2", "Mistral", 128000, 128000, 3.00, 9.00, 3.8},
		{"Qwen 2.5 72B", "Alibaba", 131072, 8192, 1.20, 1.20, 3.6},
	}
}

func main() {
	defaultIn := "Explain quantum computing in simple terms with examples and analogies for a curious beginner."
	defaultOut := "Quantum computing uses qubits, which can be 0, 1, or both at once via superposition and entanglement, " +
		"allowing many possibilities to be explored simultaneously before collapsing to a single answer."

	inPhrase := flag.String("in", defaultIn, "input/prompt phrase (its character count drives IN tokens)")
	outPhrase := flag.String("out", defaultOut, "output/response phrase (its character count drives OUT tokens)")
	filter := flag.String("models", "", "comma-separated model subset; empty runs all")
	width := flag.Int("bar-width", 40, "width of the ASCII progress bars")
	flag.Parse()

	models := allModels()
	if *filter != "" {
		models = filterModels(models, *filter)
		if len(models) == 0 {
			fmt.Fprintf(os.Stderr, "no models matched %q\n", *filter)
			os.Exit(1)
		}
	}

	fmt.Println("=== LLM Token Calculator: how IN / OUT tokens are computed ===")
	fmt.Println("Tokens = ceil(phrase chars / chars-per-token); output capped by MaxOutput,")
	fmt.Println("total checked against ContextWindow; cost = tokens * model price per 1M.")
	fmt.Printf("\nSame phrases, %d model variation(s) - each tokenizer splits text differently,\n"+
		"so the same text yields different IN/OUT token counts and costs per model.\n\n", len(models))
	fmt.Printf("  IN  phrase: %q  (%d chars)\n", *inPhrase, len(*inPhrase))
	fmt.Printf("  OUT phrase: %q  (%d chars)\n", *outPhrase, len(*outPhrase))

	fmt.Println()
	fmt.Println("Sample command (phrase + models):")
	fmt.Printf("  go run . -in \"%s\" -out \"%s\" -models \"GPT-4o mini,Claude Haiku 4,Gemini 1.5 Flash\"\n",
		shorten(*inPhrase, 60), shorten(*outPhrase, 60))
	fmt.Println()

	estimates := make([]TokenEstimate, 0, len(models))
	for _, m := range models {
		estimates = append(estimates, demoRequest(m, *inPhrase, *outPhrase, *width))
	}

	printSummary(models, estimates)
}
