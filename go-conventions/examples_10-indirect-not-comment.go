package main

import (
	"fmt"
	// This is a direct import — it will NOT be marked indirect
	"golang.org/x/exp/slog"
)

func main() {
	slog.Info("test")
	fmt.Println("check go.mod — is slog marked indirect?")
}