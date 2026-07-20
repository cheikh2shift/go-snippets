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
	runtime.GC()
	time.Sleep(60 * time.Second)
}
