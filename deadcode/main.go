// A small demo program for the official deadcode tool.
package main

import (
	"fmt"
	"strings"
)

func legacyHelper(s string) string {
	return strings.ToUpper(s)
}

func process(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(process("world"))
}
