package main

import (
	"fmt"
	"os"
)

// ❌ WRONG: defer in a loop
func processWrong(files []string) {
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		defer file.Close() // stacks up until function returns
		// process file...
	}
}

// ✅ RIGHT: scoped function or explicit close
func processRight(files []string) {
	for _, f := range files {
		func() {
			file, err := os.Open(f)
			if err != nil {
				return
			}
			defer file.Close() // defers until this closure returns
			// process file...
		}()
	}
}

func main() {
	files := []string{"a.txt", "b.txt", "c.txt"}
	processRight(files)
	fmt.Println("done — no leak")
}