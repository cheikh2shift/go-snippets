package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// ❌ WRONG: only client timeout
func clientWrong() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

// ✅ RIGHT: client + transport timeouts
func clientRight() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			IdleConnTimeout: 90 * time.Second,
			MaxIdleConns: 100,
		},
	}
}

func main() {
	client := clientRight()
	resp, err := client.Get("https://httpbin.org/status/200")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("status:", resp.Status, "body:", len(body))
}