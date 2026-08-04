package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryOn429(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	oldEndpoint := chatEndpoint
	chatEndpoint = server.URL
	defer func() { chatEndpoint = oldEndpoint }()

	c := &Client{
		APIKey:     "test",
		Pricing:    PricingCache{},
		MaxTokens:  16,
		Retries:    2,
		RetryDelay: 10 * time.Millisecond,
		HTTP:       &http.Client{Timeout: 5 * time.Second},
	}

	resp, _, err := c.runChat("test/model", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("runChat with retry failed: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("content = %v", resp.Choices[0].Message.Content)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 rate-limited + 1 retry), got %d", calls)
	}
}

func TestRetryExhausted(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	oldEndpoint := chatEndpoint
	chatEndpoint = server.URL
	defer func() { chatEndpoint = oldEndpoint }()

	c := &Client{
		APIKey:     "test",
		Pricing:    PricingCache{},
		MaxTokens:  16,
		Retries:    2,
		RetryDelay: 5 * time.Millisecond,
		HTTP:       &http.Client{Timeout: 5 * time.Second},
	}

	_, _, err := c.runChat("test/model", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", calls)
	}
}

func TestNoRetryOnOtherErrors(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	oldEndpoint := chatEndpoint
	chatEndpoint = server.URL
	defer func() { chatEndpoint = oldEndpoint }()

	c := &Client{
		APIKey:     "test",
		Pricing:    PricingCache{},
		MaxTokens:  16,
		Retries:    2,
		RetryDelay: 5 * time.Millisecond,
		HTTP:       &http.Client{Timeout: 5 * time.Second},
	}

	_, _, err := c.runChat("test/model", ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for non-429")
	}
	if calls != 1 {
		t.Errorf("expected no retry on non-429, got %d calls", calls)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	_, err := json.Marshal(ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQAGrading(t *testing.T) {
	cases := []struct {
		task QATask
		out  string
		want bool
	}{
		{QATask{Kind: "exact", Answers: []string{"paris"}}, "Paris is the capital.", true},
		{QATask{Kind: "exact", Answers: []string{"paris"}}, "london", false},
		{QATask{Kind: "exact", Answers: []string{"william shakespeare", "shakespeare"}}, "William Shakespeare wrote it.", true},
		{QATask{Kind: "numeric", Target: 144, Tol: 0.01}, "144", true},
		{QATask{Kind: "numeric", Target: 144, Tol: 0.01}, "144.2", false},
		{QATask{Kind: "numeric", Target: 12.56636, Tol: 0.05}, "12.57", true},
		{QATask{Kind: "multi", Answers: []string{"b", "B"}}, "B", true},
		{QATask{Kind: "multi", Answers: []string{"b", "B"}}, "Jupiter", false},
		{QATask{Kind: "numeric", Target: 100, Tol: 0.5}, "100 degrees Celsius", true},
	}
	for i, c := range cases {
		got, _ := c.task.grade(c.out)
		if got != c.want {
			t.Errorf("case %d: grade(%q) = %v, want %v", i, c.out, got, c.want)
		}
	}
}

func TestSentenceCount(t *testing.T) {
	if n := sentenceCount("One. Two! Three?"); n != 3 {
		t.Errorf("sentenceCount = %d, want 3", n)
	}
	if n := sentenceCount("Just one sentence here"); n != 1 {
		t.Errorf("sentenceCount = %d, want 1", n)
	}
}

func TestExtractToolCalls(t *testing.T) {
	text := `I will look that up. {"name": "get_weather", "arguments": {"location": "Paris", "unit": "celsius"}}`
	calls := extractToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "get_weather" {
		t.Errorf("name = %s", calls[0].Function.Name)
	}
	args, err := toolArgs(calls[0])
	if err != nil {
		t.Fatal(err)
	}
	if args["location"] != "Paris" {
		t.Errorf("location = %v", args["location"])
	}
}

func TestMatchCalls(t *testing.T) {
	call := func(name string, args string) ToolCall {
		var tc ToolCall
		tc.Function.Name = name
		tc.Function.Arguments = args
		return tc
	}
	actual := []ToolCall{
		call("get_stock_price", `{"ticker": "AAPL"}`),
		call("get_stock_price", `{"ticker": "MSFT"}`),
		call("get_stock_price", `{"ticker": "GOOG"}`),
	}
	expected := []ExpectedCall{
		{Tool: "get_stock_price", Args: map[string]any{"ticker": "GOOG"}},
		{Tool: "get_stock_price", Args: map[string]any{"ticker": "AAPL"}},
		{Tool: "get_stock_price", Args: map[string]any{"ticker": "MSFT"}},
	}
	pass, acc, _ := matchCalls(actual, expected)
	if !pass || acc != 1.0 {
		t.Errorf("matchCalls = (%v, %.2f), want (true, 1)", pass, acc)
	}

	actual2 := []ToolCall{call("get_weather", `{"location": "Tokyo"}`)}
	_, acc2, _ := matchCalls(actual2, expected)
	if acc2 != 0.0 {
		t.Errorf("mismatched tools should give 0 arg accuracy, got %.2f", acc2)
	}
}

func TestArgEqual(t *testing.T) {
	if !argEqual(7.0, 7.0) {
		t.Error("7.0 != 7.0")
	}
	if !argEqual(7.0, 7) {
		t.Error("7.0 != 7 (int)")
	}
	if !argEqual(9, 9.0) {
		t.Error("9 (int) != 9.0")
	}
	if !argEqual("1234*56", "1234 * 56") {
		t.Error("expression normalization failed")
	}
	if argEqual("apple", "orange") {
		t.Error("apple == orange")
	}
}

func TestJSONGrading(t *testing.T) {
	task := JSONTask{Schema: map[string]JSProp{
		"status": {Type: "string", Required: true, Enum: []string{"success", "error"}},
		"code":   {Type: "integer", Required: true},
	}}
	pass, _ := task.grade(`{"status": "success", "code": 200}`)
	if !pass {
		t.Error("valid JSON should pass")
	}
	pass, _ = task.grade(`{"status": "maybe", "code": 200}`)
	if pass {
		t.Error("enum violation should fail")
	}
	pass, _ = task.grade(`{"status": "success"}`)
	if pass {
		t.Error("missing required key should fail")
	}
	pass, _ = task.grade(`{"status": "success", "code": 2.5}`)
	if pass {
		t.Error("non-integer code should fail")
	}
}

func TestJSONFenceStrip(t *testing.T) {
	got := extractJSON("```json\n{\"a\": 1}\n```")
	if !strings.Contains(got, "a") {
		t.Errorf("extractJSON = %q", got)
	}
}

func TestInstrCheckers(t *testing.T) {
	c := func(f func(string) (bool, string), out string, want bool) {
		got, _ := f(out)
		if got != want {
			t.Errorf("checker(%q) = %v, want %v", out, got, want)
		}
	}
	c(sentenceCountCheck3, "A. B. C.", true)
	c(sentenceCountCheck3, "A. B.", false)
	c(noAndCheck, "It is simple.", true)
	c(noAndCheck, "It is simple and clear.", false)
	c(wordCount3050, strings.Repeat("word ", 40), true)
	c(wordCount3050, strings.Repeat("word ", 10), false)
	c(noDigitsCheck, "wake, eat, work.", true)
	c(noDigitsCheck, "wake at 7am.", false)
	c(jsonKeysCheck, `{"name": "x", "age": 3}`, true)
	c(jsonKeysCheck, `{"name": "x"}`, false)
}

var (
	sentenceCountCheck3 = func(out string) (bool, string) {
		if n := sentenceCount(out); n == 3 {
			return true, ""
		} else {
			return false, ""
		}
	}
	noAndCheck = func(out string) (bool, string) {
		return !wordIn("and", out), ""
	}
	wordCount3050 = func(out string) (bool, string) {
		n := len(strings.Fields(out))
		return n >= 30 && n <= 50, ""
	}
	noDigitsCheck = func(out string) (bool, string) {
		return !digitRe.MatchString(out), ""
	}
	jsonKeysCheck = func(out string) (bool, string) {
		obj, err := jsonUnmarshalObject(extractJSON(out))
		if err != nil {
			return false, ""
		}
		ks := sortedKeys(obj)
		return len(ks) == 2 && ks[0] == "age" && ks[1] == "name", ""
	}
)
