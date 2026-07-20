package main

import (
	"encoding/hex"
	"os"
	"runtime"
	"time"
)

func main() {
	hexStr := os.Getenv("CANARY_HEX")
	c, _ := hex.DecodeString(hexStr)
	runtime.KeepAlive(c)
	time.Sleep(60 * time.Second)
}
