package main

import (
	"fmt"
	"reflect"
)

// --- 1.18: The Foundation (Type Parameters) ---
// Simple generic function to compare two values
func Equal[T comparable](a, b T) bool {
	return a == b
}

// --- 1.21: The Standard Library (slices/maps) ---
// Using slices.Contains (simulated)
func Contains[T comparable](s []T, target T) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// --- 1.27: The Evolution (Generic Methods) ---
// This is the key feature of 1.27: Methods with their own type parameters
type Container[T any] struct {
	Value T
}

// Generic method: T is already bound, but U is new!
func (c *Container[T]) Transform[U any](fn func(T) U) *Container[U] {
	return &Container[U]{Value: fn(c.Value)}
}

func main() {
	fmt.Println("--- Go Generics Evolution Experiment ---")

	// 1.18 Test
	fmt.Printf("1.18 Equal: %v\n", Equal(10, 10))

	// 1.21 Test
	nums := []int{1, 2, 3}
	fmt.Printf("1.21 Contains: %v\n", Contains(nums, 2))

	// 1.27 Test
	c := &Container[int]{Value: 10}
	// Transform int to string
	strC := c.Transform(func(i int) string {
		return fmt.Sprintf("Value is %d", i)
	})
	
	fmt.Printf("1.27 Transform: %s (Type: %s)\n", strC.Value, reflect.TypeOf(strC.Value))
}
