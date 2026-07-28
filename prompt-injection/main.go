package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

func extractText(htmlInput string) string {
	reScript := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)
	reStyle := regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`)
	text := reScript.ReplaceAllString(htmlInput, "")
	text = reStyle.ReplaceAllString(text, "")

	text = html.UnescapeString(text)

	reTags := regexp.MustCompile(`<[^>]+>`)
	text = reTags.ReplaceAllString(text, " ")

	text = strings.Join(strings.Fields(text), " ")
	text = stripZeroWidthChars(text)
	return text
}

func main() {
	data, err := os.ReadFile("sample_injection.html")
	if err != nil {
		log.Fatalf("Failed to read input file: %v", err)
	}

	htmlContent := string(data)
	rawText := extractText(htmlContent)

	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("  STEP 1: Raw text extracted from the webpage")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println(rawText)
	fmt.Println()

	start := time.Now()
	escaper := NewEscaper()
	clean, detections := escaper.SanitizeConcurrent(rawText)
	elapsed := time.Since(start)
	log.Printf("Sanitized %d bytes in %v - found %d detections", len(rawText), elapsed, len(detections))

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
			fmt.Printf("      Action: %s\n", d.Replacement)
			if d.DecodedPayload != "" {
				fmt.Printf("      Decoded: %s\n", d.DecodedPayload)
			}
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
