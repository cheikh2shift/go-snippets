package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"simd" // Using the experimental Go 1.27 SIMD package
)

// Config
const MaxModelSize = 300 * 1024 * 1024 // 300MB limit
const ChunkSize = 256                  // Process in chunks for low memory footprint

// Matrix represents a flattened weight matrix
type Matrix struct {
	Data []float32
	Rows int
	Cols int
}

// LoadWeights reads binary weights with a memory safety check
func LoadWeights(path string) ([]float32, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, _ := file.Stat()
	if info.Size() > MaxModelSize {
		return nil, fmt.Errorf("model size %d exceeds 300MB limit", info.Size())
	}

	weights := make([]float32, info.Size()/4)
	err = binary.Read(file, binary.LittleEndian, &weights)
	return weights, err
}

// SIMDMultiply performs vector-matrix multiplication using SIMD chunks
func SIMDMultiply(vec []float32, matrix []float32, cols int) []float32 {
	rows := len(matrix) / cols
	result := make([]float32, rows)

	for i := 0; i < rows; i++ {
		var sum float32 = 0
		rowOffset := i * cols

		// Process in chunks using SIMD
		for j := 0; j < cols; j += ChunkSize {
			end := j + ChunkSize
			if end > cols {
				end = cols
			}

			// SIMD operation: Dot product of vector chunk and matrix row chunk
			// This leverages Go 1.27's archsimd for hardware acceleration
			sum += simd.DotProduct(vec[j:end], matrix[rowOffset+j:rowOffset+end])
		}
		result[i] = sum
	}
	return result
}

// Softmax converts logits to probabilities
func Softmax(logits []float32) []float32 {
	maxLogit := logits[0]
	for _, v := range logits {
		if v > maxLogit {
			maxLogit = v
		}
	}

	exp := make([]float32, len(logits))
	var sum float32
	for i, v := range logits {
		exp[i] = float32(math.Exp(float64(v - maxLogit)))
		sum += exp[i]
	}

	for i := range exp {
		exp[i] /= sum
	}
	return exp
}

func main() {
	fmt.Println("🚀 Go-Native SIMD Inference Engine (Go 1.27)")
	fmt.Println("Memory Limit: 300MB | Chunked Processing Enabled")

	// Placeholder for actual weights
	weights, _ := LoadWeights("model.bin")
	//weights := make([]float32, 1024*1024) // Mock weights

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\nPrompt > ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		if input == "exit" {
			break
		}

		// Simulate inference
		inputVec := make([]float32, 1024) // Mock input vector
		output := SIMDMultiply(inputVec, weights, 1024)
		probs := Softmax(output)

		fmt.Printf("Inference complete. Top probability index: %v\n", probs[0])

		// Force GC to keep footprint low
		runtime.GC()
	}
}
