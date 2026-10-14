package main

import (
	"fmt"
	"time"
)

// ❌ WRONG: interface for config
type ConfigWrong interface {
	GetTimeout() time.Duration
	GetRetries() int
}

// ✅ RIGHT: struct with tags
type Config struct {
	Timeout time.Duration `default:"30s"`
	Retries int           `default:"3"`
}

func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("invalid timeout")
	}
	return nil
}

func main() {
	c := Config{Timeout: 5 * time.Second, Retries: 3}
	if err := c.Validate(); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("config valid")
}