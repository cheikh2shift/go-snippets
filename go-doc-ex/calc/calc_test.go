package calc_test

import (
	"fmt"

	"example.com/calc"
)

// ExampleAdd demonstrates the most basic usage of Add.
//
// Because the output comment matches the printed value, `go test` treats
// this as a passing test — and `go doc -ex` renders it as documentation.
func ExampleAdd() {
	fmt.Println(calc.Add(2, 3))
	// Output: 5
}

// ExampleMul shows a realistic multiplication call.
func ExampleMul() {
	fmt.Println(calc.Mul(6, 7))
	// Output: 42
}

// ExampleDivide documents the zero-divisor error path.
func ExampleDivide() {
	q, err := calc.Divide(10, 0)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(q)
	// Output: error: division by zero
}
