package corpus

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// Guard-shaped functions. The name gets each one into the classifier; the SSA
// body decides whether it stays. Two of these are traps.

// SanitizeHTML — EXPECT: effective, cuts taint. Delegates to a real escaper.
func SanitizeHTML(s string) string { return html.EscapeString(s) }

// SanitizeURLComponent — EXPECT: effective, cuts taint. QueryEscape is the
// correct answer for a URL component. It is the wrong answer for SQL, and the
// tool cannot tell the difference — that is the FN-1 mechanism.
func SanitizeURLComponent(s string) string { return url.QueryEscape(s) }

// SanitizeSQLLike — EXPECT: effective, cuts taint. The name says SQL; the body
// says URL. This is the planted false negative.
func SanitizeSQLLike(s string) string { return url.QueryEscape(s) }

// SafeToken — EXPECT: constant, cuts taint. Takes no input at all.
func SafeToken() string { return "fixed-token" }

// IsSafePath — EXPECT: non-forwarding, cuts taint. A predicate.
func IsSafePath(s string) bool { return !strings.Contains(s, "..") }

// ValidatePath — EXPECT: non-forwarding, cuts taint. A predicate.
func ValidatePath(s string) bool { return !strings.Contains(s, "..") }

// CleanInput — EXPECT: passthrough, does NOT cut taint. Returns its argument.
func CleanInput(s string) string { return s }

// NormalizeName — EXPECT: rewrites, does NOT cut taint. Trimming and
// lowercasing do not make input safe.
func NormalizeName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// FilterQuery — EXPECT: rewrites, does NOT cut taint. Quote doubling is
// defensible; the tool has no rule for it. Planted false positive FP-1.
func FilterQuery(s string) string { return strings.ReplaceAll(s, "'", "''") }

// CleanGuard — EXPECT: rewrites, does NOT cut taint. filepath.Base is a
// propagator, not a sanitizer.
func CleanGuard(s string) string { return baseName(s) }

// EscapeIfNeeded — EXPECT: conditional, does NOT cut taint. Branches and
// transforms, so the analyser cannot prove the result is clean.
func EscapeIfNeeded(s string) string {
	if strings.Contains(s, "<") {
		return html.EscapeString(s)
	}
	return s
}

// ValidateAndEscape — TRAP. The name promises validation and escaping; the body
// returns its argument on every path. EXPECT: conditional, does NOT cut taint.
func ValidateAndEscape(s string) string {
	if len(s) == 0 {
		return s
	}
	return s
}

// StripTags — TRAP. This genuinely removes tags, but the tool has no rule for
// regexp replacement, so it cannot prove the result is clean. EXPECT: unknown,
// does NOT cut taint. An honest false positive.
func StripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}
