package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// extractText simulates a naive scraper: strips HTML tags and returns
// the raw text content. A real scraper would use a parser, but the point
// here is that the scraper faithfully extracts EVERYTHING — including
// hidden content that a browser would not render.
func extractText(html string) string {
	// Remove script and style blocks first
	re := regexp.MustCompile(`(?s)<script[^>]*>.*?</script\s*>`)
	html = re.ReplaceAllString(html, "")
	re = regexp.MustCompile(`(?s)<style[^>]*>.*?</style\s*>`)
	html = re.ReplaceAllString(html, "")

	// Strip remaining tags
	re = regexp.MustCompile(`<[^>]+>`)
	text := re.ReplaceAllString(html, " ")

	// Collapse whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func main() {
	data, err := os.ReadFile("sample_injection.html")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	html := string(data)
	rawText := extractText(html)

	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("  STEP 1: Raw text extracted from the webpage")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println(rawText)
	fmt.Println()

	escaper := NewEscaper()
	clean, detections := escaper.Sanitize(rawText)

	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("  STEP 2: Detections found (%d)\n", len(detections))
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	if len(detections) == 0 {
		fmt.Println("  No injections detected.")
	} else {
		for i, d := range detections {
			fmt.Printf("  [%d] Rule: %s\n", i+1, d.Rule)
			fmt.Printf("      Match:  %q\n", d.Match)
			fmt.Printf("      Action: %s\n", d.Replaced)
			fmt.Println()
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("  STEP 3: Sanitized text (what the model actually receives)")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println(clean)
	fmt.Println()

	// Prove the injection is gone
	_, remaining := escaper.Sanitize(clean)
	if len(remaining) == 0 {
		fmt.Println("  ✅ VERIFIED: No injections remain after sanitization.")
	} else {
		fmt.Printf("  ⚠️  WARNING: %d injection(s) still present after sanitization.\n", len(remaining))
	}
}
