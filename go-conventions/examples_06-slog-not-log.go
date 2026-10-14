package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/exp/slog"
)

// ❌ WRONG: unstructured logging
func handlerWrong(ctx context.Context, model string, tokens int) {
	log.Printf("request for model=%s tokens=%d", model, tokens)
}

// ✅ RIGHT: structured logging
func handlerRight(ctx context.Context, model string, tokens int) {
	slog.Info("request completed",
		"model", model,
		"tokens", tokens,
		"duration", time.Since(time.Now()),
	)
}

func main() {
	// Configure JSON handler for production
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(handler))

	handlerRight(context.Background(), "llama-3", 42)
}