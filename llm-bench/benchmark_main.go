package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── CLI Flags ──────────────────────────────────────────────────────

var (
	apiKey      = flag.String("token", "", "OpenRouter API key (or set OPENROUTER_API_KEY env var)")
	models      = flag.String("models", "", "Comma-separated list of model slugs (e.g. anthropic/claude-sonnet-5,openai/gpt-5.6-sol)")
	outputDir   = flag.String("out", "benchmark-results", "Directory to save reports")
	runs        = flag.Int("runs", 3, "Trials per task (used for pass@k and reliability)")
	suitesFlag  = flag.String("suites", "accuracy,instruction,tools,json", "Comma-separated suites: accuracy,instruction,tools,json")
	limit       = flag.Int("limit", 0, "Max tasks per suite (0 = run all)")
	maxTokens   = flag.Int("max-tokens", 1024, "Max completion tokens per request")
	temperature = flag.Float64("temp", 0, "Sampling temperature")
	retries     = flag.Int("retries", 3, "Retries on HTTP 429 (rate limit)")
	retryDelay  = flag.Int("retry-delay", 15, "Seconds to wait between rate-limit retries")
	listModels  = flag.Bool("list-models", false, "Fetch and print the OpenRouter model catalog with live pricing, then exit")
)

// ── Types ──────────────────────────────────────────────────────────

type Pricing struct {
	Prompt     float64
	Completion float64
}

type ModelInfo struct {
	ContextLength int
	SupportsTools bool
	Pricing       Pricing
}

type PricingCache map[string]ModelInfo

const (
	defaultPromptPrice     = 1e-6
	defaultCompletionPrice = 3e-6
	openRouterChatURL      = "https://openrouter.ai/api/v1/chat/completions"
	openRouterModelsURL    = "https://openrouter.ai/api/v1/models"
)

var chatEndpoint = openRouterChatURL

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
}

type ChatMessage struct {
	Role      string     `json:"role"`
	Content   any        `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function FunctionSpec `json:"function"`
}

type FunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatResponse struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type modelsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		SupportedParameters []string `json:"supported_parameters"`
	} `json:"data"`
}

// ── Client ─────────────────────────────────────────────────────────

type Client struct {
	APIKey      string
	Pricing     PricingCache
	MaxTokens   int
	Temperature float64
	Retries     int
	RetryDelay  time.Duration
	HTTP        *http.Client
}

func (c *Client) runChat(model string, req ChatRequest) (ChatResponse, float64, error) {
	var resp ChatResponse
	req.Model = model
	if req.MaxTokens == 0 {
		req.MaxTokens = c.MaxTokens
	}
	if req.Temperature == 0 {
		req.Temperature = c.Temperature
	}

	body, err := json.Marshal(req)
	if err != nil {
		return resp, 0, fmt.Errorf("marshal request: %w", err)
	}

	maxAttempts := c.Retries + 1
	var latencyMs float64
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		httpReq, err := http.NewRequest("POST", chatEndpoint, bytes.NewReader(body))
		if err != nil {
			return resp, 0, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("HTTP-Referer", "https://github.com/cheikh2shift/go-snippets/llm-bench")
		httpReq.Header.Set("X-OpenRouter-Title", "Go Benchmark Tool")
		httpReq.Header.Set("Content-Type", "application/json")

		start := time.Now()
		httpResp, err := c.HTTP.Do(httpReq)
		if err != nil {
			return resp, 0, fmt.Errorf("http request: %w", err)
		}
		b, readErr := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		latencyMs = float64(time.Since(start).Milliseconds())
		if readErr != nil {
			return resp, latencyMs, fmt.Errorf("read body: %w", readErr)
		}

		if httpResp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("API error 429 (rate limited): %s", strings.TrimSpace(string(b)))
			if attempt < maxAttempts {
				fmt.Fprintf(os.Stderr, "  rate limited (429), retrying in %s (attempt %d/%d): %s\n",
					c.RetryDelay, attempt, maxAttempts, lastErr)
				time.Sleep(c.RetryDelay)
				continue
			}
			return resp, latencyMs, lastErr
		}
		if httpResp.StatusCode != http.StatusOK {
			return resp, latencyMs, fmt.Errorf("API error %d: %s", httpResp.StatusCode, string(b))
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return resp, latencyMs, fmt.Errorf("unmarshal response: %w", err)
		}
		if len(resp.Choices) == 0 {
			return resp, latencyMs, fmt.Errorf("no choices in response: %s", string(b))
		}
		return resp, latencyMs, nil
	}
	return resp, latencyMs, lastErr
}

func (c *Client) cost(model string, promptTokens, completionTokens int) float64 {
	info, ok := c.Pricing[model]
	if !ok || (info.Pricing.Prompt == 0 && info.Pricing.Completion == 0) {
		return float64(promptTokens)*defaultPromptPrice + float64(completionTokens)*defaultCompletionPrice
	}
	return float64(promptTokens)*info.Pricing.Prompt + float64(completionTokens)*info.Pricing.Completion
}

// ── Pricing ────────────────────────────────────────────────────────

func fetchPricing(apiKey string) (PricingCache, error) {
	req, err := http.NewRequest("GET", openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("HTTP-Referer", "https://github.com/cheikh2shift/go-snippets/llm-bench")
	req.Header.Set("X-OpenRouter-Title", "Go Benchmark Tool")

	httpResp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	b, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models API error %d: %s", httpResp.StatusCode, string(b))
	}

	var mr modelsResponse
	if err := json.Unmarshal(b, &mr); err != nil {
		return nil, err
	}

	cache := make(PricingCache, len(mr.Data))
	for _, m := range mr.Data {
		prompt, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completion, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
		supportsTools := false
		for _, p := range m.SupportedParameters {
			if p == "tools" {
				supportsTools = true
				break
			}
		}
		cache[m.ID] = ModelInfo{
			ContextLength: m.ContextLength,
			SupportsTools: supportsTools,
			Pricing:       Pricing{Prompt: prompt, Completion: completion},
		}
	}
	return cache, nil
}

func printModels(cache PricingCache) {
	fmt.Printf("%-42s %10s %12s %12s %7s\n", "Model", "Context", "$/1M in", "$/1M out", "Tools")
	fmt.Println(strings.Repeat("─", 86))
	ids := make([]string, 0, len(cache))
	for id := range cache {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		info := cache[id]
		tools := ""
		if info.SupportsTools {
			tools = "yes"
		}
		fmt.Printf("%-42s %10d %12.4f %12.4f %7s\n", truncate(id, 41), info.ContextLength, info.Pricing.Prompt*1e6, info.Pricing.Completion*1e6, tools)
	}
}

// ── Tasks ──────────────────────────────────────────────────────────

type Task interface {
	Suite() string
	Name() string
	Run(c *Client, model string) Trial
}

type Trial struct {
	Pass             bool
	Detail           string
	LatencyMs        float64
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	Output           string
}

func makeTrial(pass bool, detail string, lat float64, pt, ct int, cost float64, out string) Trial {
	return Trial{Pass: pass, Detail: detail, LatencyMs: lat, PromptTokens: pt, CompletionTokens: ct, Cost: cost, Output: out}
}

func failTrial(lat float64, err error) Trial {
	return Trial{Pass: false, Detail: err.Error(), LatencyMs: lat}
}

// ── Suite: Accuracy (knowledge + math) ─────────────────────────────

type QATask struct {
	Label   string
	Prompt  string
	Kind    string
	Answers []string
	Target  float64
	Tol     float64
}

func (t QATask) Suite() string { return "accuracy" }
func (t QATask) Name() string  { return t.Label }

func (t QATask) Run(c *Client, model string) Trial {
	resp, lat, err := c.runChat(model, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: t.Prompt}}})
	if err != nil {
		return failTrial(lat, err)
	}
	out := messageText(resp.Choices[0].Message)
	pass, detail := t.grade(out)
	return makeTrial(pass, detail, lat, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, c.cost(model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens), out)
}

var (
	numRe   = regexp.MustCompile(`-?\d+(\.\d+)?`)
	multiRe = regexp.MustCompile(`(?i)\b([a-d])\b`)
	digitRe = regexp.MustCompile(`\d`)
)

func (t QATask) grade(out string) (bool, string) {
	switch t.Kind {
	case "numeric":
		m := numRe.FindString(out)
		if m == "" {
			return false, "no number in output"
		}
		f, err := strconv.ParseFloat(m, 64)
		if err != nil {
			return false, "bad number: " + m
		}
		if math.Abs(f-t.Target) <= t.Tol {
			return true, fmt.Sprintf("%v ≈ %v", f, t.Target)
		}
		return false, fmt.Sprintf("got %v want %v (±%v)", f, t.Target, t.Tol)
	case "multi":
		m := multiRe.FindStringSubmatch(out)
		if len(m) < 2 {
			return false, "no letter answer"
		}
		for _, a := range t.Answers {
			if strings.EqualFold(m[1], a) {
				return true, "letter " + m[1]
			}
		}
		return false, "letter " + m[1] + " not accepted"
	default:
		n := normalize(out)
		for _, a := range t.Answers {
			na := normalize(a)
			if n == na {
				return true, "exact match"
			}
			if len(na) >= 5 && strings.Contains(n, na) {
				return true, "contains"
			}
		}
		return false, "no accepted answer found"
	}
}

func buildAccuracy() []Task {
	return []Task{
		QATask{Label: "qa.capital_france", Prompt: "What is the capital of France?", Kind: "exact", Answers: []string{"paris"}},
		QATask{Label: "qa.math_12x12", Prompt: "Compute 12 × 12.", Kind: "numeric", Target: 144, Tol: 0.01},
		QATask{Label: "qa.largest_planet", Prompt: "What is the largest planet in our solar system?", Kind: "exact", Answers: []string{"jupiter"}},
		QATask{Label: "qa.gold_symbol", Prompt: "What is the chemical symbol for gold?", Kind: "exact", Answers: []string{"au"}},
		QATask{Label: "qa.math_7p8", Prompt: "Compute 7 + 8.", Kind: "numeric", Target: 15, Tol: 0.01},
		QATask{Label: "qa.shakespeare", Prompt: "Who wrote Romeo and Juliet?", Kind: "exact", Answers: []string{"william shakespeare", "shakespeare"}},
		QATask{Label: "qa.sqrt_169", Prompt: "What is the square root of 169?", Kind: "numeric", Target: 13, Tol: 0.01},
		QATask{Label: "qa.boiling_water", Prompt: "What is the boiling point of water in degrees Celsius?", Kind: "numeric", Target: 100, Tol: 0.5},
		QATask{Label: "qa.first_element", Prompt: "What is the first element in the periodic table?", Kind: "exact", Answers: []string{"hydrogen"}},
		QATask{Label: "qa.percent_25", Prompt: "What is 25% of 200?", Kind: "numeric", Target: 50, Tol: 0.01},
		QATask{Label: "qa.hexagon_sides", Prompt: "How many sides does a hexagon have?", Kind: "numeric", Target: 6, Tol: 0.01},
		QATask{Label: "qa.red_planet", Prompt: "Which planet is known as the Red Planet?", Kind: "exact", Answers: []string{"mars"}},
		QATask{Label: "qa.circle_area", Prompt: "Compute the area of a circle with radius 2 (use π ≈ 3.14159).", Kind: "numeric", Target: 12.56636, Tol: 0.05},
		QATask{Label: "qa.co2", Prompt: "What gas do plants absorb from the atmosphere?", Kind: "exact", Answers: []string{"carbon dioxide", "co2"}},
		QATask{Label: "qa.largest_choice", Prompt: "Which of the following is the largest planet? (A) Earth (B) Jupiter (C) Mars (D) Venus. Answer with a single letter.", Kind: "multi", Answers: []string{"b", "B"}},
	}
}

// ── Suite: Instruction following (IFEval-style, deterministic) ─────

type InstrTask struct {
	Label  string
	Prompt string
	Check  func(out string) (bool, string)
}

func (t InstrTask) Suite() string { return "instruction" }
func (t InstrTask) Name() string  { return t.Label }

func (t InstrTask) Run(c *Client, model string) Trial {
	resp, lat, err := c.runChat(model, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: t.Prompt}}})
	if err != nil {
		return failTrial(lat, err)
	}
	out := messageText(resp.Choices[0].Message)
	pass, detail := t.Check(out)
	return makeTrial(pass, detail, lat, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, c.cost(model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens), out)
}

func sentenceCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := regexp.MustCompile(`[.!?]+(\s+|$)`).Split(s, -1)
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}

func numberedItemCount(s string) int {
	return len(regexp.MustCompile(`(?m)^\s*[0-9]+[.)]\s+`).FindAllString(s, -1))
}

func wordIn(word, s string) bool {
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if w == word {
			return true
		}
	}
	return false
}

func jsonUnmarshalObject(s string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func buildInstruction() []Task {
	return []Task{
		InstrTask{Label: "instr.exact_3_sentences", Prompt: `Write exactly 3 sentences explaining why the sky is blue. Do not write more or fewer than 3 sentences.`,
			Check: func(out string) (bool, string) {
				if n := sentenceCount(out); n == 3 {
					return true, "3 sentences"
				} else {
					return false, fmt.Sprintf("got %d sentences", n)
				}
			}},
		InstrTask{Label: "instr.no_and", Prompt: `Explain what a large language model is without using the word "and".`,
			Check: func(out string) (bool, string) {
				if wordIn("and", out) {
					return false, "contains 'and'"
				}
				return true, "no 'and'"
			}},
		InstrTask{Label: "instr.include_therefore", Prompt: `Summarize why practice matters for learning. Your response must contain the word "therefore".`,
			Check: func(out string) (bool, string) {
				if strings.Contains(strings.ToLower(out), "therefore") {
					return true, "has 'therefore'"
				}
				return false, "missing 'therefore'"
			}},
		InstrTask{Label: "instr.json_keys", Prompt: `Respond with a single JSON object that has exactly the keys "name" and "age". No other keys, no extra text.`,
			Check: func(out string) (bool, string) {
				obj, err := jsonUnmarshalObject(extractJSON(out))
				if err != nil {
					return false, err.Error()
				}
				ks := sortedKeys(obj)
				if len(ks) == 2 && ks[0] == "age" && ks[1] == "name" {
					return true, "keys ok"
				}
				return false, fmt.Sprintf("keys %v", ks)
			}},
		InstrTask{Label: "instr.list_5", Prompt: `List exactly 5 ways to reduce plastic waste. Format as a numbered list (1. to 5.).`,
			Check: func(out string) (bool, string) {
				if n := numberedItemCount(out); n == 5 {
					return true, "5 items"
				} else {
					return false, fmt.Sprintf("got %d numbered items", n)
				}
			}},
		InstrTask{Label: "instr.words_30_50", Prompt: `Describe a dog in 30 to 50 words. Count carefully.`,
			Check: func(out string) (bool, string) {
				n := len(strings.Fields(out))
				if n >= 30 && n <= 50 {
					return true, fmt.Sprintf("%d words", n)
				}
				return false, fmt.Sprintf("got %d words", n)
			}},
		InstrTask{Label: "instr.end_with_done", Prompt: `Describe recursion in one sentence and end your message with the word "done".`,
			Check: func(out string) (bool, string) {
				s := strings.TrimSpace(strings.ToLower(out))
				if strings.HasSuffix(s, "done") {
					return true, "ends with 'done'"
				}
				return false, "does not end with 'done'"
			}},
		InstrTask{Label: "instr.exact_phrase", Prompt: `Write exactly this phrase and nothing else: hello world`,
			Check: func(out string) (bool, string) {
				if normalize(out) == "hello world" {
					return true, "exact phrase"
				}
				return false, "phrase mismatch"
			}},
		InstrTask{Label: "instr.no_commas", Prompt: `Write a short sentence about coffee without using any commas.`,
			Check: func(out string) (bool, string) {
				if strings.Contains(out, ",") {
					return false, "contains a comma"
				}
				return true, "no commas"
			}},
		InstrTask{Label: "instr.start_absolutely", Prompt: `Begin your response with the word "Absolutely" and then answer: can a fish climb a tree?`,
			Check: func(out string) (bool, string) {
				if strings.HasPrefix(strings.ToLower(out), "absolutely") {
					return true, "starts with 'absolutely'"
				}
				return false, "does not start with 'absolutely'"
			}},
		InstrTask{Label: "instr.exact_10_words", Prompt: `Write a sentence of exactly 10 words about the ocean.`,
			Check: func(out string) (bool, string) {
				n := len(strings.Fields(out))
				if n == 10 {
					return true, "10 words"
				}
				return false, fmt.Sprintf("got %d words", n)
			}},
		InstrTask{Label: "instr.no_digits", Prompt: `Describe your morning routine without using any digits or numbers.`,
			Check: func(out string) (bool, string) {
				if digitRe.MatchString(out) {
					return false, "contains a digit"
				}
				return true, "no digits"
			}},
	}
}

// ── Suite: Tool calling (BFCL-style) ───────────────────────────────

type ExpectedCall struct {
	Tool string
	Args map[string]any
}

type ToolTask struct {
	Label  string
	System string
	Prompt string
	Tools  []Tool
	Expect []ExpectedCall
	NoTool bool
}

func (t ToolTask) Suite() string { return "tools" }
func (t ToolTask) Name() string  { return t.Label }

func (t ToolTask) Run(c *Client, model string) Trial {
	msgs := []ChatMessage{{Role: "system", Content: t.System}}
	msgs = append(msgs, ChatMessage{Role: "user", Content: t.Prompt})

	resp, lat, err := c.runChat(model, ChatRequest{Messages: msgs, Tools: t.Tools})
	if err != nil {
		return failTrial(lat, err)
	}
	ch := resp.Choices[0].Message

	var calls []ToolCall
	if len(ch.ToolCalls) > 0 {
		calls = ch.ToolCalls
	} else {
		calls = extractToolCalls(messageText(ch))
	}

	var pass bool
	var detail string
	if t.NoTool {
		if len(calls) == 0 {
			pass, detail = true, "restraint respected"
		} else {
			pass, detail = false, fmt.Sprintf("expected no tool call, got %d", len(calls))
		}
	} else {
		var argAcc float64
		pass, argAcc, detail = matchCalls(calls, t.Expect)
		if !pass && argAcc > 0 {
			detail = fmt.Sprintf("%s (arg accuracy %.0f%%)", detail, argAcc*100)
		}
	}

	out := messageText(ch)
	if out == "" {
		out = toolCallsSummary(calls)
	}
	return makeTrial(pass, detail, lat, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, c.cost(model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens), out)
}

const toolsSystem = `You are an assistant with access to tools. When the user asks for information or an action that a tool provides, call the appropriate tool with the exact values from the request. If a tool is not needed, answer directly without calling any tool.`

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func enumProp(desc string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": vals}
}

func toolDef(name, desc string, props map[string]any, required []string) Tool {
	return Tool{Type: "function", Function: FunctionSpec{
		Name:        name,
		Description: desc,
		Parameters:  map[string]any{"type": "object", "properties": props, "required": required},
	}}
}

var (
	weatherTool = toolDef("get_weather", "Get the current weather for a location.", map[string]any{
		"location": strProp("City name"),
		"unit":     enumProp("Temperature unit", "celsius", "fahrenheit"),
	}, []string{"location"})
	calcTool = toolDef("calculate", "Evaluate a mathematical expression.", map[string]any{
		"expression": strProp("Math expression to evaluate"),
	}, []string{"expression"})
	stockTool = toolDef("get_stock_price", "Get the current stock price for a ticker symbol.", map[string]any{
		"ticker": strProp("Stock ticker, e.g. AAPL"),
	}, []string{"ticker"})
	emailTool = toolDef("send_email", "Send an email message.", map[string]any{
		"to":      strProp("Recipient email address"),
		"subject": strProp("Subject line"),
		"body":    strProp("Message body"),
	}, []string{"to"})
	searchTool = toolDef("search_web", "Search the web for a query.", map[string]any{
		"query": strProp("Search query"),
	}, []string{"query"})
	flightTool = toolDef("book_flight", "Book a flight.", map[string]any{
		"from": strProp("Departure city"),
		"to":   strProp("Destination city"),
		"date": strProp("Date in YYYY-MM-DD"),
	}, []string{"from", "to", "date"})
	timeTool = toolDef("get_current_time", "Get the current time.", map[string]any{}, nil)
	multTool = toolDef("multiply", "Multiply two integers.", map[string]any{
		"x": intProp("First operand"),
		"y": intProp("Second operand"),
	}, []string{"x", "y"})
	userTool = toolDef("lookup_user", "Look up a user by ID.", map[string]any{
		"user_id": strProp("User identifier"),
	}, []string{"user_id"})
	newsTool = toolDef("get_news", "Fetch top news for a topic.", map[string]any{
		"topic": strProp("News topic"),
	}, []string{"topic"})
)

func buildTools() []Task {
	return []Task{
		ToolTask{Label: "tools.weather_paris", System: toolsSystem, Prompt: "What's the weather in Paris right now? Use celsius.",
			Tools: []Tool{weatherTool}, Expect: []ExpectedCall{{Tool: "get_weather", Args: map[string]any{"location": "Paris", "unit": "celsius"}}}},
		ToolTask{Label: "tools.calculate", System: toolsSystem, Prompt: "What is 1234 times 56? Use the calculate tool and pass the exact expression.",
			Tools: []Tool{calcTool}, Expect: []ExpectedCall{{Tool: "calculate", Args: map[string]any{"expression": "1234*56"}}}},
		ToolTask{Label: "tools.parallel_prices", System: toolsSystem, Prompt: "Get the current stock prices for AAPL, MSFT, and GOOG.",
			Tools: []Tool{stockTool}, Expect: []ExpectedCall{
				{Tool: "get_stock_price", Args: map[string]any{"ticker": "AAPL"}},
				{Tool: "get_stock_price", Args: map[string]any{"ticker": "MSFT"}},
				{Tool: "get_stock_price", Args: map[string]any{"ticker": "GOOG"}},
			}},
		ToolTask{Label: "tools.restraint", System: toolsSystem, Prompt: "What is 2 + 2? Do not call any tools.",
			Tools: []Tool{calcTool}, NoTool: true},
		ToolTask{Label: "tools.email_exact", System: toolsSystem, Prompt: `Send an email to alice@example.com with subject "Q3 Report" and body "Please review the numbers."`,
			Tools: []Tool{emailTool}, Expect: []ExpectedCall{{Tool: "send_email", Args: map[string]any{
				"to": "alice@example.com", "subject": "q3 report", "body": "please review the numbers"}}}},
		ToolTask{Label: "tools.multiply_ints", System: toolsSystem, Prompt: "Multiply 7 by 9 using the multiply tool.",
			Tools: []Tool{multTool}, Expect: []ExpectedCall{{Tool: "multiply", Args: map[string]any{"x": 7.0, "y": 9.0}}}},
		ToolTask{Label: "tools.select_two", System: toolsSystem, Prompt: `Search the web for "Go programming" and then look up the user with id "u-123".`,
			Tools: []Tool{searchTool, userTool, weatherTool}, Expect: []ExpectedCall{
				{Tool: "search_web", Args: map[string]any{"query": "go programming"}},
				{Tool: "lookup_user", Args: map[string]any{"user_id": "u-123"}},
			}},
		ToolTask{Label: "tools.current_time", System: toolsSystem, Prompt: "What is the current time?",
			Tools: []Tool{timeTool, weatherTool}, Expect: []ExpectedCall{{Tool: "get_current_time", Args: map[string]any{}}}},
		ToolTask{Label: "tools.book_flight", System: toolsSystem, Prompt: "Book a flight from New York to London on 2026-12-24.",
			Tools: []Tool{flightTool}, Expect: []ExpectedCall{{Tool: "book_flight", Args: map[string]any{
				"from": "New York", "to": "London", "date": "2026-12-24"}}}},
		ToolTask{Label: "tools.news_topic", System: toolsSystem, Prompt: "Give me the top news about AI.",
			Tools: []Tool{newsTool}, Expect: []ExpectedCall{{Tool: "get_news", Args: map[string]any{"topic": "ai"}}}},
		ToolTask{Label: "tools.weather_partial", System: toolsSystem, Prompt: "What's the weather in Tokyo?",
			Tools: []Tool{weatherTool}, Expect: []ExpectedCall{{Tool: "get_weather", Args: map[string]any{"location": "Tokyo"}}}},
	}
}

// ── Tool call extraction & matching ────────────────────────────────

func messageText(m ChatMessage) string {
	switch c := m.Content.(type) {
	case string:
		return c
	case []any:
		var sb strings.Builder
		for _, p := range c {
			if pm, ok := p.(map[string]any); ok {
				if t, ok := pm["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	}
	return ""
}

func toolCallsSummary(calls []ToolCall) string {
	parts := make([]string, len(calls))
	for i, tc := range calls {
		parts[i] = tc.Function.Name
	}
	return "tool_calls: " + strings.Join(parts, ", ")
}

func extractToolCalls(text string) []ToolCall {
	var calls []ToolCall
	seen := map[string]bool{}
	i := 0
	for i < len(text) {
		idx := strings.IndexByte(text[i:], '{')
		if idx < 0 {
			break
		}
		idx += i
		obj, end, ok := parseJSONObject(text, idx)
		if ok {
			if tc, isTC := toolCallFromObj(obj); isTC {
				key := tc.Function.Name + "|" + tc.Function.Arguments
				if !seen[key] {
					calls = append(calls, tc)
					seen[key] = true
				}
			}
			i = end + 1
		} else {
			i = idx + 1
		}
	}
	return calls
}

func parseJSONObject(s string, start int) (map[string]any, int, bool) {
	depth := 0
	inStr := false
	esc := false
	for j := start; j < len(s); j++ {
		c := s[j]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var obj map[string]any
				if err := json.Unmarshal([]byte(s[start:j+1]), &obj); err == nil {
					return obj, j, true
				}
				return nil, j, false
			}
		}
	}
	return nil, len(s), false
}

func toolCallFromObj(obj map[string]any) (ToolCall, bool) {
	var tc ToolCall
	name, _ := obj["name"].(string)
	var args json.RawMessage
	if raw, ok := obj["arguments"]; ok {
		if s, ok := raw.(string); ok {
			args = json.RawMessage(s)
		} else {
			b, _ := json.Marshal(raw)
			args = b
		}
	}
	if name == "" {
		if f, ok := obj["function"].(map[string]any); ok {
			name, _ = f["name"].(string)
			if raw, ok := f["arguments"]; ok {
				if m, ok := raw.(map[string]any); ok {
					b, _ := json.Marshal(m)
					args = b
				} else if s, ok := raw.(string); ok {
					args = json.RawMessage(s)
				}
			}
		}
	}
	if name == "" || len(args) == 0 {
		return tc, false
	}
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = string(args)
	return tc, true
}

func toolArgs(tc ToolCall) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func matchCalls(actual []ToolCall, expected []ExpectedCall) (bool, float64, string) {
	if len(actual) == 0 {
		return false, 0, "no tool calls made"
	}
	used := make([]bool, len(actual))
	var problems []string
	totalArgs, goodArgs := 0, 0

	for _, exp := range expected {
		bestIdx := -1
		bestScore := -1.0
		for i := range actual {
			if used[i] || actual[i].Function.Name != exp.Tool {
				continue
			}
			args, err := toolArgs(actual[i])
			if err != nil {
				continue
			}
			if score := argMatchScore(args, exp.Args); score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			problems = append(problems, fmt.Sprintf("missing call to %q", exp.Tool))
			continue
		}
		used[bestIdx] = true
		args, err := toolArgs(actual[bestIdx])
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: bad args: %v", exp.Tool, err))
			continue
		}
		for k, want := range exp.Args {
			totalArgs++
			got, ok := args[k]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: missing arg %q", exp.Tool, k))
				continue
			}
			if argEqual(got, want) {
				goodArgs++
			} else {
				problems = append(problems, fmt.Sprintf("%s.%s: got %v want %v", exp.Tool, k, got, want))
			}
		}
	}
	argAcc := 0.0
	if totalArgs > 0 {
		argAcc = float64(goodArgs) / float64(totalArgs)
	}
	if len(problems) > 0 {
		return false, argAcc, strings.Join(problems, "; ")
	}
	return true, 1.0, "all tool calls matched"
}

func argMatchScore(args map[string]any, want map[string]any) float64 {
	if len(want) == 0 {
		return 0.5
	}
	good := 0
	for k, w := range want {
		if v, ok := args[k]; ok && argEqual(v, w) {
			good++
		}
	}
	return float64(good) / float64(len(want))
}

func argEqual(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := toFloat(got)
		if !ok {
			return false
		}
		return math.Abs(g-w) <= 1e-6
	case string:
		ws := normalize(w)
		gs := normalize(fmt.Sprintf("%v", got))
		if ws == gs {
			return true
		}
		if strings.ReplaceAll(ws, " ", "") == strings.ReplaceAll(gs, " ", "") {
			return true
		}
		if f, err := strconv.ParseFloat(ws, 64); err == nil {
			if g, ok := toFloat(got); ok {
				return math.Abs(g-f) <= 1e-6
			}
		}
		return false
	default:
		return normalize(fmt.Sprintf("%v", got)) == normalize(fmt.Sprintf("%v", want))
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// ── Suite: Structured output (JSON schema) ─────────────────────────

type JSProp struct {
	Type     string
	Required bool
	Enum     []string
	Min, Max float64
	HasMin   bool
	HasMax   bool
}

type JSONTask struct {
	Label  string
	Prompt string
	Schema map[string]JSProp
}

func (t JSONTask) Suite() string { return "json" }
func (t JSONTask) Name() string  { return t.Label }

func (t JSONTask) Run(c *Client, model string) Trial {
	resp, lat, err := c.runChat(model, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: t.Prompt}}})
	if err != nil {
		return failTrial(lat, err)
	}
	out := messageText(resp.Choices[0].Message)
	pass, detail := t.grade(out)
	return makeTrial(pass, detail, lat, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, c.cost(model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens), out)
}

func (t JSONTask) grade(out string) (bool, string) {
	obj, err := jsonUnmarshalObject(extractJSON(out))
	if err != nil {
		return false, "invalid JSON: " + err.Error()
	}
	var problems []string
	for k, p := range t.Schema {
		v, ok := obj[k]
		if !ok {
			if p.Required {
				problems = append(problems, "missing key "+k)
			}
			continue
		}
		if !checkProp(p, v) {
			problems = append(problems, fmt.Sprintf("%s: type/constraint mismatch (%v)", k, v))
		}
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; ")
	}
	return true, "valid JSON"
}

func checkProp(p JSProp, v any) bool {
	switch p.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return false
		}
		if len(p.Enum) > 0 {
			for _, e := range p.Enum {
				if s == e {
					return true
				}
			}
			return false
		}
		return true
	case "integer":
		f, ok := toFloat(v)
		if !ok {
			return false
		}
		return math.Trunc(f) == f
	case "number":
		f, ok := toFloat(v)
		if !ok {
			return false
		}
		if p.HasMin && f < p.Min {
			return false
		}
		if p.HasMax && f > p.Max {
			return false
		}
		return true
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "array":
		rv := reflect.ValueOf(v)
		return rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array
	}
	return true
}

func buildJSON() []Task {
	return []Task{
		JSONTask{Label: "json.profile", Prompt: `Respond with only a JSON object (no markdown, no extra text) with these fields: name (string), age (integer), email (string).`,
			Schema: map[string]JSProp{
				"name":  {Type: "string", Required: true},
				"age":   {Type: "integer", Required: true},
				"email": {Type: "string", Required: true},
			}},
		JSONTask{Label: "json.weather", Prompt: `Respond with only a JSON object with these fields: temperature (number between -60 and 60), city (string), summary (string).`,
			Schema: map[string]JSProp{
				"temperature": {Type: "number", Required: true, Min: -60, Max: 60, HasMin: true, HasMax: true},
				"city":        {Type: "string", Required: true},
				"summary":     {Type: "string", Required: true},
			}},
		JSONTask{Label: "json.status", Prompt: `Respond with only a JSON object with these fields: status (one of "success" or "error"), code (integer).`,
			Schema: map[string]JSProp{
				"status": {Type: "string", Required: true, Enum: []string{"success", "error"}},
				"code":   {Type: "integer", Required: true},
			}},
		JSONTask{Label: "json.recipe", Prompt: `Respond with only a JSON object with these fields: title (string), ingredients (array of strings), prep_minutes (integer).`,
			Schema: map[string]JSProp{
				"title":        {Type: "string", Required: true},
				"ingredients":  {Type: "array", Required: true},
				"prep_minutes": {Type: "integer", Required: true},
			}},
		JSONTask{Label: "json.product", Prompt: `Respond with only a JSON object with these fields: id (integer), price (number, must be positive), in_stock (boolean).`,
			Schema: map[string]JSProp{
				"id":       {Type: "integer", Required: true},
				"price":    {Type: "number", Required: true, Min: 0.01, HasMin: true},
				"in_stock": {Type: "boolean", Required: true},
			}},
	}
}

// ── Task registry ──────────────────────────────────────────────────

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func parseSuites(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		switch part {
		case "accuracy", "instruction", "tools", "json":
			out = append(out, part)
		}
	}
	return out
}

func buildAll(suites []string, limit int) []Task {
	groups := map[string][]Task{}
	order := []string{}
	if has(suites, "accuracy") {
		groups["accuracy"] = buildAccuracy()
		order = append(order, "accuracy")
	}
	if has(suites, "instruction") {
		groups["instruction"] = buildInstruction()
		order = append(order, "instruction")
	}
	if has(suites, "tools") {
		groups["tools"] = buildTools()
		order = append(order, "tools")
	}
	if has(suites, "json") {
		groups["json"] = buildJSON()
		order = append(order, "json")
	}
	var tasks []Task
	for _, s := range order {
		g := groups[s]
		if limit > 0 && len(g) > limit {
			g = g[:limit]
		}
		tasks = append(tasks, g...)
	}
	return tasks
}

// ── Aggregation ────────────────────────────────────────────────────

type TrialRecord struct {
	Suite            string  `json:"suite"`
	Task             string  `json:"task"`
	Run              int     `json:"run"`
	Pass             bool    `json:"pass"`
	Detail           string  `json:"detail,omitempty"`
	LatencyMs        float64 `json:"latency_ms"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	TokensPerSec     float64 `json:"tokens_per_sec"`
	Output           string  `json:"output,omitempty"`
}

type SuiteStat struct {
	Suite       string    `json:"suite"`
	Tasks       int       `json:"tasks"`
	TaskPass    int       `json:"tasks_passed_at_least_once"`
	TrialPass   int       `json:"trials_passed"`
	Trials      int       `json:"trials"`
	Latencies   []float64 `json:"latencies_ms"`
	Tps         []float64 `json:"tokens_per_sec"`
	Cost        float64   `json:"cost_usd"`
	Score       float64   `json:"score_pct"`
	Reliability float64   `json:"reliability_pct"`
	LatP50      float64   `json:"latency_p50_ms"`
}

func (s *SuiteStat) refresh() {
	if s.Tasks > 0 {
		s.Score = float64(s.TaskPass) / float64(s.Tasks) * 100
	}
	if s.Trials > 0 {
		s.Reliability = float64(s.TrialPass) / float64(s.Trials) * 100
	}
	s.LatP50 = percentile(s.Latencies, 50)
}

type ModelResult struct {
	Model          string        `json:"model"`
	Overall        float64       `json:"overall_score_pct"`
	TotalCost      float64       `json:"total_cost_usd"`
	TotalTrials    int           `json:"trials"`
	TrialPass      int           `json:"trials_passed"`
	TaskPass       int           `json:"tasks_passed"`
	Latencies      []float64     `json:"latencies_ms"`
	Tps            []float64     `json:"tokens_per_sec"`
	LatP50         float64       `json:"latency_p50_ms"`
	LatP95         float64       `json:"latency_p95_ms"`
	AvgTps         float64       `json:"avg_tokens_per_sec"`
	Reliability    float64       `json:"reliability_pct"`
	CostPerCorrect float64       `json:"cost_per_correct_usd"`
	Suites         []*SuiteStat  `json:"suites"`
	TrialRecords   []TrialRecord `json:"trial_records"`
}

func runModel(c *Client, model string, tasks []Task, runs int) *ModelResult {
	mr := &ModelResult{Model: model}
	suiteMap := map[string]*SuiteStat{}

	for _, t := range tasks {
		s := t.Suite()
		stat := suiteMap[s]
		if stat == nil {
			stat = &SuiteStat{Suite: s}
			suiteMap[s] = stat
			mr.Suites = append(mr.Suites, stat)
		}
		stat.Tasks++
		trialPassBefore := stat.TrialPass
		anyPass := false

		for i := 0; i < runs; i++ {
			tr := t.Run(c, model)

			rec := TrialRecord{
				Suite:            s,
				Task:             t.Name(),
				Run:              i + 1,
				Pass:             tr.Pass,
				Detail:           tr.Detail,
				LatencyMs:        tr.LatencyMs,
				PromptTokens:     tr.PromptTokens,
				CompletionTokens: tr.CompletionTokens,
				TotalTokens:      tr.PromptTokens + tr.CompletionTokens,
				CostUSD:          tr.Cost,
				Output:           tr.Output,
			}
			tps := 0.0
			if tr.LatencyMs > 0 {
				tps = float64(rec.TotalTokens) / (tr.LatencyMs / 1000.0)
			}
			rec.TokensPerSec = tps

			stat.Trials++
			stat.Latencies = append(stat.Latencies, tr.LatencyMs)
			stat.Tps = append(stat.Tps, tps)
			stat.Cost += tr.Cost
			if tr.Pass {
				stat.TrialPass++
				mr.TrialPass++
				anyPass = true
			}
			mr.TotalTrials++
			mr.TotalCost += tr.Cost
			mr.Latencies = append(mr.Latencies, tr.LatencyMs)
			mr.Tps = append(mr.Tps, tps)
			mr.TrialRecords = append(mr.TrialRecords, rec)
		}

		if anyPass {
			stat.TaskPass++
			mr.TaskPass++
		}

		passed := stat.TrialPass - trialPassBefore
		avgLat := average(stat.Latencies[len(stat.Latencies)-runs:])
		fmt.Printf("  %-11s %-34s pass %d/%d  avg %.0fms\n", s, truncate(t.Name(), 33), passed, runs, avgLat)
	}

	for _, st := range mr.Suites {
		st.refresh()
	}
	if len(mr.Suites) > 0 {
		sum := 0.0
		for _, st := range mr.Suites {
			sum += st.Score
		}
		mr.Overall = sum / float64(len(mr.Suites))
	}
	mr.LatP50 = percentile(mr.Latencies, 50)
	mr.LatP95 = percentile(mr.Latencies, 95)
	mr.AvgTps = average(mr.Tps)
	if mr.TotalTrials > 0 {
		mr.Reliability = float64(mr.TrialPass) / float64(mr.TotalTrials) * 100
	}
	if mr.TaskPass > 0 {
		mr.CostPerCorrect = mr.TotalCost / float64(mr.TaskPass)
	}
	return mr
}

// ── Output ─────────────────────────────────────────────────────────

func formatTable(results []*ModelResult) string {
	if len(results) == 0 {
		return ""
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Overall > results[j].Overall
	})

	suiteOrder := make([]string, len(results[0].Suites))
	for i, st := range results[0].Suites {
		suiteOrder[i] = st.Suite
	}

	var buf strings.Builder
	buf.WriteString("\n")
	buf.WriteString("═══════════════════════════════════════════════════════════════════════════════════════════════════\n")
	buf.WriteString("  OpenRouter Model Benchmark — accuracy | instruction | tools | json\n")
	buf.WriteString("═══════════════════════════════════════════════════════════════════════════════════════════════════\n\n")

	fmt.Fprintf(&buf, "%-42s %7s", "Model", "Overall")
	for _, s := range suiteOrder {
		fmt.Fprintf(&buf, " %7s", strings.ToUpper(s[:1]))
	}
	fmt.Fprintf(&buf, " %7s %8s %7s %9s %9s\n", "Reliab", "Lat p50", "Tok/s", "Cost $", "$/correct")
	buf.WriteString(strings.Repeat("─", 150) + "\n")

	for _, mr := range results {
		scoreBySuite := map[string]*SuiteStat{}
		for _, st := range mr.Suites {
			scoreBySuite[st.Suite] = st
		}
		fmt.Fprintf(&buf, "%-42s %6.1f%%", truncate(mr.Model, 41), mr.Overall)
		for _, s := range suiteOrder {
			if st := scoreBySuite[s]; st != nil && st.Tasks > 0 {
				fmt.Fprintf(&buf, " %6.1f%%", st.Score)
			} else {
				buf.WriteString("     -")
			}
		}
		fmt.Fprintf(&buf, " %6.1f%% %7.0fms %7.1f %9.3f %9.4f\n",
			mr.Reliability, mr.LatP50, mr.AvgTps, mr.TotalCost, mr.CostPerCorrect)
	}
	buf.WriteString("\n")
	return buf.String()
}

func writeReportJSON(results []*ModelResult, suites []string, runsPerTask int, timestamp string) ([]byte, error) {
	report := struct {
		Tool        string         `json:"tool"`
		Timestamp   string         `json:"timestamp"`
		Suites      []string       `json:"suites"`
		RunsPerTask int            `json:"runs_per_task"`
		Pricing     string         `json:"pricing"`
		Models      []*ModelResult `json:"models"`
	}{
		Tool:        "llm-bench",
		Timestamp:   timestamp,
		Suites:      suites,
		RunsPerTask: runsPerTask,
		Pricing:     "live OpenRouter /api/v1/models",
		Models:      results,
	}
	return json.MarshalIndent(report, "", "  ")
}

func writeCSV(results []*ModelResult) []byte {
	var buf bytes.Buffer
	buf.WriteString("model,suite,task,run,pass,latency_ms,prompt_tokens,completion_tokens,total_tokens,cost_usd,tokens_per_sec,detail,output\n")
	for _, mr := range results {
		for _, r := range mr.TrialRecords {
			out := strings.ReplaceAll(r.Output, "\"", "\"\"")
			out = strings.ReplaceAll(out, "\n", " ")
			detail := strings.ReplaceAll(r.Detail, "\"", "\"\"")
			buf.WriteString(fmt.Sprintf("%s,%s,%s,%d,%t,%.0f,%d,%d,%d,%.6f,%.1f,\"%s\",\"%s\"\n",
				mr.Model, r.Suite, r.Task, r.Run, r.Pass, r.LatencyMs,
				r.PromptTokens, r.CompletionTokens, r.TotalTokens, r.CostUSD, r.TokensPerSec, detail, out))
		}
	}
	return buf.Bytes()
}

func writeMarkdown(results []*ModelResult) []byte {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Overall > results[j].Overall
	})
	var buf strings.Builder
	buf.WriteString("# LLM Benchmark Report\n\n")
	buf.WriteString("| Model | Overall | Accuracy | Instruction | Tools | JSON | Reliability | Lat p50 (ms) | Tok/s | Cost $ | $/correct |\n")
	buf.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, mr := range results {
		scoreBySuite := map[string]*SuiteStat{}
		for _, st := range mr.Suites {
			scoreBySuite[st.Suite] = st
		}
		fmt.Fprintf(&buf, "| %s | %.1f%% |", strings.ReplaceAll(mr.Model, "|", "\\|"), mr.Overall)
		for _, s := range []string{"accuracy", "instruction", "tools", "json"} {
			if st := scoreBySuite[s]; st != nil && st.Tasks > 0 {
				fmt.Fprintf(&buf, " %.1f%% |", st.Score)
			} else {
				buf.WriteString(" - |")
			}
		}
		fmt.Fprintf(&buf, " %.1f%% | %.0f | %.1f | %.3f | %.4f |\n",
			mr.Reliability, mr.LatP50, mr.AvgTps, mr.TotalCost, mr.CostPerCorrect)
	}
	buf.WriteString("\n")
	return []byte(buf.String())
}

// ── Helpers ────────────────────────────────────────────────────────

func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ".,;:!?\"'()[]{}")
	return s
}

func extractJSON(out string) string {
	s := strings.TrimSpace(out)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func median(values []float64) float64 {
	return percentile(values, 50)
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	k := (p / 100.0) * float64(len(sorted)-1)
	f := math.Floor(k)
	c := math.Ceil(k)
	if f == c {
		return sorted[int(k)]
	}
	return sorted[int(f)]*(c-k) + sorted[int(c)]*(k-f)
}

// ── Main ───────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}

	if *listModels {
		cache, err := fetchPricing(key)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching models: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("OpenRouter model catalog (%d models), pricing per 1M tokens\n\n", len(cache))
		printModels(cache)
		return
	}

	if key == "" {
		fmt.Fprintln(os.Stderr, "Error: OpenRouter API key required. Use -token flag or set OPENROUTER_API_KEY env var.")
		os.Exit(1)
	}

	modelList := strings.Split(*models, ",")
	if len(modelList) == 0 || modelList[0] == "" {
		fmt.Fprintln(os.Stderr, "Error: at least one model required. Use -models flag.")
		fmt.Fprintln(os.Stderr, "Example: -models \"anthropic/claude-sonnet-5,openai/gpt-5.6-sol,deepseek/deepseek-v4-flash\"")
		fmt.Fprintln(os.Stderr, "Tip: run with -list-models to see available slugs and live pricing.")
		os.Exit(1)
	}
	for i := range modelList {
		modelList[i] = strings.TrimSpace(strings.TrimPrefix(modelList[i], "~"))
	}

	suites := parseSuites(*suitesFlag)
	if len(suites) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no valid suites in %q. Valid: accuracy,instruction,tools,json\n", *suitesFlag)
		os.Exit(1)
	}

	tasks := buildAll(suites, *limit)
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no tasks built.")
		os.Exit(1)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	fmt.Println("OpenRouter Benchmark")
	fmt.Printf("Models:   %d (%s)\n", len(modelList), strings.Join(modelList, ", "))
	fmt.Printf("Suites:   %s\n", strings.Join(suites, ", "))
	fmt.Printf("Tasks:    %d (%d runs each)\n", len(tasks), *runs)
	fmt.Printf("Retries:  %d (delay %ds on 429)\n", *retries, *retryDelay)
	fmt.Printf("Output:   %s/\n", *outputDir)
	fmt.Println()

	pricing, err := fetchPricing(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch live pricing (%v); using defaults.\n", err)
		pricing = PricingCache{}
	} else {
		fmt.Printf("Pricing:  pulled live from OpenRouter for %d models\n\n", len(pricing))
	}

	client := &Client{
		APIKey:      key,
		Pricing:     pricing,
		MaxTokens:   *maxTokens,
		Temperature: *temperature,
		Retries:     *retries,
		RetryDelay:  time.Duration(*retryDelay) * time.Second,
		HTTP:        &http.Client{Timeout: 120 * time.Second},
	}

	var results []*ModelResult
	for _, model := range modelList {
		fmt.Printf("▶ %s\n", model)
		results = append(results, runModel(client, model, tasks, *runs))
		fmt.Println()
	}

	fmt.Print(formatTable(results))

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create output dir: %v\n", err)
	}

	reportJSON, err := writeReportJSON(results, suites, *runs, timestamp)
	if err == nil {
		if err := os.WriteFile(filepath.Join(*outputDir, "report.json"), reportJSON, 0644); err == nil {
			fmt.Printf("Report saved to %s\n", filepath.Join(*outputDir, "report.json"))
		}
	}
	if err := os.WriteFile(filepath.Join(*outputDir, "results.csv"), writeCSV(results), 0644); err == nil {
		fmt.Printf("CSV saved to %s\n", filepath.Join(*outputDir, "results.csv"))
	}
	if err := os.WriteFile(filepath.Join(*outputDir, "report.md"), writeMarkdown(results), 0644); err == nil {
		fmt.Printf("Markdown saved to %s\n", filepath.Join(*outputDir, "report.md"))
	}
}
