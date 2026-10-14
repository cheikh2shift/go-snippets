package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ❌ WRONG: mutex for a counter
type CounterWrong struct {
	mu sync.Mutex
	n  int
}

func (c *CounterWrong) Inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// ✅ RIGHT: atomic for single values
type CounterRight struct {
	n atomic.Int32
}

func (c *CounterRight) Inc() {
	c.n.Add(1)
}

func main() {
	var c CounterRight
	c.Inc()
	fmt.Println("count:", c.n.Load())
}