package main

import (
	"fmt"

	"gofixexperiment/legacy"
)

func main() {
	pairs := legacy.ParsePairs([]string{"go=1.26", "fix=on"})
	fmt.Println("parsed:", pairs)

	fmt.Println("max(3,7) =", legacy.MaxOf(3, 7))
	fmt.Println("min(3,7) =", legacy.MinOf(3, 7))

	keys := legacy.MapKeys(map[string]int{"a": 1, "b": 2})
	fmt.Println("keys:", keys)

	buf := legacy.Logf("hello %s", "world")
	fmt.Println("log:", string(buf))

	fmt.Println("interface:", legacy.OldInterface(42))
}
