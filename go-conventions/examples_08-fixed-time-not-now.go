package main

import (
	"fmt"
	"testing"
	"time"
)

// ❌ WRONG: time.Now() in tests
func calculateAgeWrong(birthDate time.Time) int {
	return int(time.Since(birthDate).Hours() / 24 / 365)
}

// ✅ RIGHT: fixed time
func calculateAge(birthDate, now time.Time) int {
	return int(now.Sub(birthDate).Hours() / 24 / 365)
}

func main() {
	// In tests, you control the clock:
	birth := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	age := calculateAge(birth, now)
	fmt.Println("age:", age)
}