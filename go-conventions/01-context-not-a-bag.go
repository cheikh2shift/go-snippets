package main

import (
	"context"
	"fmt"
	"time"
)

// ❌ WRONG: stuff the user ID in context
func handlerWrong(ctx context.Context) {
	// How do you know this value is here?
	// What if the caller forgot?
	// What if it's stale from a previous request?
	userID, _ := ctx.Value("user_id").(string)
	fmt.Println("user:", userID)
}

// ✅ RIGHT: pass it explicitly
func handlerRight(ctx context.Context, userID string) {
	// The signature tells you exactly what this function needs.
	// No type assertions. No silent failures.
	fmt.Println("user:", userID)
}

func main() {
	// The context still carries cancellation — that's its real job.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	<-ctx.Done()
	fmt.Println("timeout fired — this is what context is for")
}