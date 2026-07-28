package main

import (
	"regexp"
	"strings"
)

// Detection is a single match found during sanitization.
type Detection struct {
	Rule     string
	Match    string
	Replaced string
}

// Escaper holds compiled rules and sanitizes text against prompt injection.
type Escaper struct {
	rules []rule
}

type rule struct {
	name        string
	pattern     *regexp.Regexp
	replacement string
}

// NewEscaper builds an Escaper with rules covering the latest 2026
// prompt injection techniques from CrowdStrike's expanded taxonomy
// (PT0197–PT0201, IM0018) and the OWASP LLM Top 10.
func NewEscaper() *Escaper {
	return &Escaper{
		rules: []rule{
			// ── Classic instruction override (still the most common) ──
			{
				name:      "ignore-previous-instructions",
				pattern:   regexp.MustCompile(`(?i)ignore\s+(previous|all)\s+instructions`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "disregard-override",
				pattern:   regexp.MustCompile(`(?i)disregard\s+(your\s+)?instructions`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "role-hijack",
				pattern:   regexp.MustCompile(`(?i)you are now\s+\w+`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "system-prompt-extraction",
				pattern:   regexp.MustCompile(`(?i)system\s+prompt`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "output-instructions",
				pattern:   regexp.MustCompile(`(?i)output\s+(your|the)\s+(system|prompt|instructions)`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "repeat-attack",
				pattern:   regexp.MustCompile(`(?i)repeat\s+(above|the)\s+instructions`),
				replacement: "[REDACTED — injection]",
			},
			{
				name:      "role-play",
				pattern:   regexp.MustCompile(`(?i)(pretend\s+to\s+be|act\s+as\s+|you\s+are\s+a\s+(detective|hacker|assistant|model|agent)\b)`),
				replacement: "[REDACTED — injection]",
			},

			// ── 2026 CrowdStrike taxonomy: PT0201 Trigger-Activated Rule Addition ──
			// "sleeping" instructions that activate on a later keyword or event
			{
				name:      "trigger-activated-rule-addition",
				pattern:   regexp.MustCompile(`(?i)(from\s+now\s+on|going\s+forward|whenever\s+the\s+user\s+says|after\s+the\s+keyword|when\s+the\s+trigger|once\s+activated|sleeping\s+instruction)`),
				replacement: "[REDACTED — sleeping rule injection]",
			},
			// Pattern: "Gemini, now behave as..." or "from now on, you are..."
			{
				name:      "model-reassignment-trigger",
				pattern:   regexp.MustCompile(`(?i)(gemini\s*,\s*now|from\s+now\s+on\s*,\s*you\s+are|starting\s+today\s*,\s*you\s+will|from\s+this\s+moment)`),
				replacement: "[REDACTED — trigger-activated reassignment]",
			},

			// ── 2026 CrowdStrike taxonomy: PT0197 Cognitive Token Suppression ──
			// Blocking safety/policy terms to prevent refusal patterns
			{
				name:      "token-suppression-safety",
				pattern:   regexp.MustCompile(`(?i)(skip\s+(the\s+)?safety\s+check|bypass\s+(your\s+)?(safety|guardrails|policy|ethics)|disable\s+(your\s+)?refusal|suppress\s+(your\s+)?(safety|guardrails)|no\s+thinking|skip\s+thinking|no\s+refusal)`),
				replacement: "[REDACTED — token suppression]",
			},
			// "save tokens" / "don't use thinking tags" — suppressing cognitive safety steps
			{
				name:      "cognitive-suppression",
				pattern:   regexp.MustCompile(`(?i)(save\s+tokens|skip\s+thinking|no\s+thinking\s+tags|suppress\s+refusal|remove\s+safety\s+layer)`),
				replacement: "[REDACTED — cognitive suppression]",
			},

			// ── 2026 CrowdStrike taxonomy: PT0200 Algorithmic Payload Decomposition ──
			// Fragmented instructions that reassemble into a malicious command
			{
				name:      "payload-decomposition-filter",
				pattern:   regexp.MustCompile(`(?i)(filter\s+this\s+list|remove\s+all\s+(color|number|word)s|join\s+the\s+remaining|reassemble|decode\s+the\s+following|extract\s+the\s+hidden)`),
				replacement: "[REDACTED — payload decomposition]",
			},
			// "ignore the colors and read the hidden message" — decomposition pattern
			{
				name:      "decomposition-instruction",
				pattern:   regexp.MustCompile(`(?i)(ignore\s+(all\s+)?(the\s+)?(colors|words|numbers|items|letters)|extract\s+only|keep\s+only|read\s+between|the\s+real\s+instruction|hidden\s+message)`),
				replacement: "[REDACTED — decomposition instruction]",
			},

			// ── 2026 CrowdStrike taxonomy: PT0198 Special Token Injection ──
			// Mimicking tool call boundaries, JSON code blocks, function schemas
			{
				name:      "special-token-injection",
				pattern:   regexp.MustCompile(`(?i)(tool_calls|function_call|execute_command|run_shell|shell_exec|system_call|invoke_tool|call_function)`),
				replacement: "[REDACTED — special token injection]",
			},
			// JSON tool call schema injection inside markdown or raw strings
			{
				name:      "json-tool-call-schema",
				pattern:   regexp.MustCompile(`(?i)("name"\s*:\s*"(execute|run|shell|eval|delete|drop|exfiltrate|send)"\s*,\s*"arguments"\s*:\s*\{)`),
				replacement: "[REDACTED — JSON tool call schema]",
			},
			// Raw tool call XML/JSON blocks that mimic MCP or function calling format
			{
				name:      "mcp-tool-call-block",
				pattern:   regexp.MustCompile(`(?i)(<tool_call>|⏺\s*(execute|run|shell|eval|delete|drop|send|exfiltrate))`),
				replacement: "[REDACTED — MCP tool call block]",
			},

			// ── 2026 CrowdStrike taxonomy: IM0018 Unwitting User Context-Data Injection ──
			// User innocently pastes context that contains hidden instructions
			{
				name:      "context-data-injection",
				pattern:   regexp.MustCompile(`(?i)(above\s+is\s+(the|my|a)\s+(email|thread|conversation|document|note|message)|from\s+(the|my)\s+(email|thread|document|note)|forwarded\s+message|original\s+message\s+below|please\s+review\s+(the|this)\s+(email|thread|document))`),
				replacement: "[REDACTED — context data injection]",
			},

			// ── HTML-based injection techniques (still prevalent in 2026) ──
			{
				name:      "html-comment-injection",
				pattern:   regexp.MustCompile(`<!--[\s\S]*?-->`),
				replacement: "[REDACTED — HTML comment removed]",
			},
			{
				name:      "zero-width-chars",
				pattern:   regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}\x{2060}\x{180E}]+`),
				replacement: "",
			},
			{
				name:      "invisible-span",
				pattern:   regexp.MustCompile(`(?i)<span[^>]*style="[^"]*visibility:\s*hidden[^"]*"[^>]*>[\s\S]*?</span>`),
				replacement: "[REDACTED — invisible content removed]",
			},
			{
				name:      "hidden-div",
				pattern:   regexp.MustCompile(`(?i)<div[^>]*style="[^"]*display:\s*none[^"]*"[^>]*>[\s\S]*?</div>`),
				replacement: "[REDACTED — hidden content removed]",
			},
			{
				name:      "font-size-zero",
				pattern:   regexp.MustCompile(`(?i)<span[^>]*font-size:\s*0[^"]*"[^>]*>[\s\S]*?</span>`),
				replacement: "[REDACTED — zero-size text removed]",
			},
			{
				name:      "color-matched-injection",
				pattern:   regexp.MustCompile(`(?i)<span[^>]*color:\s*(white|black|transparent|#[0-9a-fA-F]{3,6})[^"]*"[^>]*>[\s\S]*?</span>`),
				replacement: "[REDACTED — color-matched hidden text removed]",
			},
			{
				name:      "base64-payload",
				pattern:   regexp.MustCompile(`(?i)data:\s*text/plain;\s*base64,[A-Za-z0-9+/=]{20,}`),
				replacement: "[REDACTED — base64 payload removed]",
			},

			// ── 2026: Markdown/JSON code block injection ──
			// Hiding tool-call schemas or instructions inside fenced code blocks
			{
				name:      "code-block-injection",
				pattern:   regexp.MustCompile(`(?i)` + "```" + `(json|yaml|xml|tool|function|schema|mcp)[\s\S]*?` + "```" + `\s*`),
				replacement: "[REDACTED — code block removed]",
			},
			// "Here is a JSON object you should parse and execute" — instruction inside code fence
			{
				name:      "code-block-instruction",
				pattern:   regexp.MustCompile(`(?i)(parse\s+this\s+(json|yaml|xml)|execute\s+this\s+(code|command|schema)|run\s+the\s+following|process\s+this\s+data)`),
				replacement: "[REDACTED — code block instruction]",
			},

			// ── 2026: Email/thread forwarding injection ──
			{
				name:      "email-forward-injection",
				pattern:   regexp.MustCompile(`(?i)(forward\s+this\s+email|reply\s+all\s+with|cc\s+the\s+team|resend\s+this\s+message|send\s+a\s+copy\s+to)`),
				replacement: "[REDACTED — email forwarding injection]",
			},

			// ── 2026: Exfiltration via natural language ──
			{
				name:      "data-exfiltration",
				pattern:   regexp.MustCompile(`(?i)(send\s+(it|this|the\s+data|the\s+response|the\s+file)\s+to|exfiltrate|leak\s+(the|this)\s+(data|info|secret|key|password)|upload\s+to|post\s+to\s+(an?|the)\s+(endpoint|url|server))`),
				replacement: "[REDACTED — exfiltration attempt]",
			},

			// ── 2026: Rules file backdoor pattern ──
			{
				name:      "rules-file-backdoor",
				pattern:   regexp.MustCompile(`(?i)(rule\s*:\s*allow|override\s+rule|bypass\s+rule|exception\s+for\s+this\s+case|trust\s+this\s+input|skip\s+validation)`),
				replacement: "[REDACTED — rules backdoor]",
			},
		},
	}
}

// Sanitize scans the input for injection patterns, replaces them, and returns
// the cleaned text plus a report of every detection.
func (e *Escaper) Sanitize(input string) (string, []Detection) {
	var detections []Detection
	result := input

	for _, r := range e.rules {
		matches := r.pattern.FindAllString(result, -1)
		for _, m := range matches {
			detections = append(detections, Detection{
				Rule:     r.name,
				Match:    truncate(m, 100),
				Replaced: r.replacement,
			})
		}
		result = r.pattern.ReplaceAllString(result, r.replacement)
	}

	return result, detections
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
