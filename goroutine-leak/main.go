package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"
)

// leakyGoroutine spawns a goroutine that blocks forever on a channel,
// then lets that channel become unreachable. In Go 1.26, the
// "goroutineleak" profile can detect this pattern.
func leakyGoroutine() {
	ch := make(chan struct{})
	go func() {
		<-ch // blocks forever; nothing ever sends on ch
	}()
	time.Sleep(1 * time.Second)
	// ch is now unreachable (no references left) -> the goroutine is leaked
}

func leakyGoroutineAlt() {
	ch := make(chan struct{})
	go func() {
		<-ch // blocks forever; nothing ever sends on ch
	}()
	time.Sleep(1 * time.Second)
}

func main() {
	// Enable the experimental goroutineleak profile (Go 1.26).
	leakyGoroutine()
	leakyGoroutine()
	// Alt adds different entry to profile to demonstrate that multiple entries can be present.
	leakyGoroutineAlt()

	// Give the runtime time to run GC and perform reachability analysis
	// so the leak can be observed.
	time.Sleep(5 * time.Second)

	fmt.Println("=== goroutineleak profile ===")
	if p := pprof.Lookup("goroutineleak"); p != nil {
		if err := p.WriteTo(os.Stdout, 1); err != nil {
			fmt.Fprintln(os.Stderr, "write failed:", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "goroutineleak profile not available")
	}
}
