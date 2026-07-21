package legacy

import (
	"fmt"
	"strings"
)

// This file is written in deliberately "old" Go idioms.
// Run `go fix ./...` and watch the modernizers rewrite it.

// parsePairs splits "k=v" entries using the pre-1.18 idiom.
func ParsePairs(pairs []string) map[string]string {
	result := make(map[string]string)
	for _, pair := range pairs {
		eq := strings.IndexByte(pair, '=')
		if eq >= 0 {
			result[pair[:eq]] = pair[eq+1:]
		}
	}
	return result
}

// maxOf returns the larger of two ints using an if/else ladder.
func MaxOf(a, b int) int {
	var x int
	if a > b {
		x = a
	} else {
		x = b
	}
	return x
}

// minOf returns the smaller of two ints using an if/else ladder.
func MinOf(a, b int) int {
	var x int
	if a < b {
		x = a
	} else {
		x = b
	}
	return x
}

// mapKeys gathers map keys into a slice with an explicit loop.
func MapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// logf builds a []byte via fmt.Sprintf then appends — the pre-1.19 way.
func Logf(format string, args ...any) []byte {
	return []byte(fmt.Sprintf(format, args...))
}

// oldInterface uses the pre-1.18 spelling of the empty interface.
func OldInterface(v interface{}) string {
	return fmt.Sprintf("%v", v)
}
