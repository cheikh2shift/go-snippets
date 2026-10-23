package main

import (
	"fmt"
	"time"
)

// Pattern 1: time.Now — capture start time for latency measurement
func p1() {
	start := time.Now()
	time.Sleep(20 * time.Millisecond) // simulate agent work
	elapsed := time.Since(start)
	fmt.Printf("1. time.Now + time.Since: agent took %v\n", elapsed)
}

// Pattern 2: time.After — timeout with a channel
func p2() {
	select {
	case <-time.After(50 * time.Millisecond):
		fmt.Println("2. time.After: task completed before timeout")
	case <-time.After(30 * time.Millisecond):
		fmt.Println("2. time.After: TIMEOUT — task exceeded 50ms")
	}
}

// Pattern 3: time.AfterFunc — schedule a callback
func p3() {
	done := make(chan bool)
	start := time.Now()
	time.AfterFunc(50*time.Millisecond, func() {
		elapsed := time.Since(start)
		fmt.Printf("3. time.AfterFunc: scheduled callback fired after %v\n", elapsed)
		done <- true
	})
	<-done
}

// Pattern 4: time.NewTicker — periodic task
func p4() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for i := 0; i < 3; i++ {
		start := time.Now()
		<-ticker.C
		fmt.Printf("4. time.NewTicker: tick %d at %v (waited %v)\n",
			i+1, time.Now().Format("15:04:05.000"), time.Since(start))
	}
}

// Pattern 5: time.NewTimer — one-shot scheduled event
func p5() {
	start := time.Now()
	timer := time.NewTimer(80 * time.Millisecond)
	<-timer.C
	fmt.Printf("5. time.NewTimer: one-shot event fired after %v\n", time.Since(start))
}

// Pattern 6: time.Tick — simpler periodic (no Stop)
func p6() {
	tick := time.Tick(120 * time.Millisecond)
	for i := 0; i < 2; i++ {
		start := time.Now()
		<-tick
		fmt.Printf("6. time.Tick: tick %d at %v (waited %v)\n",
			i+1, time.Now().Format("15:04:05.000"), time.Since(start))
	}
}

// Pattern 7: time.Sleep — simple delay
func p7() {
	start := time.Now()
	time.Sleep(60 * time.Millisecond)
	fmt.Printf("7. time.Sleep: delayed %v\n", time.Since(start))
}

// Pattern 8: time.Unix / time.UnixNano — epoch timestamps
func p8() {
	now := time.Now()
	fmt.Printf("8. time.Unix: epoch=%d, nano=%d, formatted=%s\n",
		now.Unix(), now.UnixNano(), now.Format(time.RFC3339))
}

// Pattern 9: Retry with exponential backoff
func p9() {
	start := time.Now()
	for i := 0; i < 3; i++ {
		backoff := time.Duration(1<<uint(i)) * 10 * time.Millisecond
		fmt.Printf("9. Retry backoff: attempt %d, waiting %v (elapsed %v)\n",
			i+1, backoff, time.Since(start))
		time.Sleep(backoff)
	}
	fmt.Printf("9. Retry backoff: all attempts done, total %v\n", time.Since(start))
}

// Pattern 10: Graceful shutdown with timeout
func p10() {
	start := time.Now()
	shutdown := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond) // simulate work
		close(shutdown)
	}()

	select {
	case <-shutdown:
		fmt.Printf("10. Graceful shutdown: completed cleanly in %v\n", time.Since(start))
	case <-time.After(100 * time.Millisecond):
		fmt.Printf("10. Graceful shutdown: TIMEOUT after %v — forced exit\n", time.Since(start))
	}
}

func main() {
	fmt.Println("=== 10 Go Time Patterns for agent scheduling ===\n")
	p1()
	p2()
	p3()
	p4()
	p5()
	p6()
	p7()
	p8()
	p9()
	p10()
	fmt.Println("\nAll patterns demonstrated.")
}