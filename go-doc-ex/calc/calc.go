// Package calc provides a small set of arithmetic helpers.
//
// Every function below has a matching Example function in calc_test.go.
// Those examples are run by `go test` AND rendered by `go doc -ex`.
package calc

import "errors"

// Add returns the sum of a and b.
//
// It is the simplest function in the package and exists mainly to
// demonstrate how a doc comment pairs with an example.
func Add(a, b int) int { return a + b }

// Mul returns the product of a and b.
//
// Unlike Add, Mul is not commutative in the presence of overflow, so the
// example shows a realistic call rather than a trivial one.
func Mul(a, b int) int { return a * b }

// Divide returns the integer quotient of a and b.
//
// It returns an error when b is zero, which is the interesting edge case
// that the example is designed to document.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}
