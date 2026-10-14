package main

import (
	"fmt"
	"os"
)

// ❌ WRONG: panic for validation
func parseConfigWrong(data []byte) {
	if len(data) == 0 {
		panic("empty config") // crashes the whole process
	}
}

// ✅ RIGHT: return error
func parseConfigRight(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty config")
	}
	return nil
}

func main() {
	err := parseConfigRight([]byte{})
	if err != nil {
		fmt.Println("handled:", err)
		os.Exit(1) // only exit when you mean to
	}
}