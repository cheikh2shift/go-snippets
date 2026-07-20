package main

import (
	"encoding/hex"
	"os"
	"runtime"
	"runtime/secret"
	"time"
)

func main() {
	hexStr := os.Getenv("CANARY_HEX")

	secret.Do(func() {
		c, _ := hex.DecodeString(hexStr)
		runtime.KeepAlive(c)
	})

	runtime.GC()
	time.Sleep(60 * time.Second)
}
