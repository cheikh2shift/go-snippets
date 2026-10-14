package main

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("not found")

// ❌ WRONG: string matching breaks on refactoring
func findWrong(err error) bool {
	return strings.Contains(err.Error(), "not found")
}

// ✅ RIGHT: sentinel errors + errors.Is
func findRight(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// The wrap pattern that preserves the chain
func fetch() error {
	err := queryDatabase()
	if err != nil {
		// Wrap with context, but keep the original
		return fmt.Errorf("fetch user: %w", err)
	}
	return nil
}

func queryDatabase() error {
	return ErrNotFound
}

func main() {
	err := fetch()
	if errors.Is(err, ErrNotFound) {
		fmt.Println("handled correctly")
	}
}