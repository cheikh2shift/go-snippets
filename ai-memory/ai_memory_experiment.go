package main

import (
	"container/heap"
	"container/list"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"strings"
)

// --- 1. Priority Context (container/heap) ---
type MemoryItem struct {
	ID       int
	Content  string
	Priority int
	Vector   []float64
	index    int
}

type PriorityQueue []*MemoryItem

func (pq PriorityQueue) Len() int           { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool { return pq[i].Priority > pq[j].Priority } // Max-heap
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*MemoryItem)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// --- 2. Sliding Window Context (container/list) ---

type SlidingWindow struct {
	queue    *list.List
	capacity int
}

func NewSlidingWindow(capacity int) *SlidingWindow {
	return &SlidingWindow{queue: list.New(), capacity: capacity}
}

func (sw *SlidingWindow) Add(item *MemoryItem) {
	if sw.queue.Len() >= sw.capacity {
		sw.queue.Remove(sw.queue.Front())
	}
	sw.queue.PushBack(item)
}

// --- 3. Vectorization & Similarity (math) ---

// TextToVector is a simple Bag-of-Words vectorizer
func TextToVector(text string) []float64 {
	// Simple vocabulary for demonstration
	vocab := []string{"database", "connection", "fix", "leak", "system", "user"}
	vec := make([]float64, len(vocab))
	words := strings.Fields(strings.ToLower(text))
	for _, word := range words {
		for i, v := range vocab {
			if strings.Contains(word, v) {
				vec[i]++
			}
		}
	}
	return vec
}

func CosineSimilarity(a, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// --- 4. Persistence (encoding/gob) ---

type AgentMemory struct {
	LongTerm []*MemoryItem
}

func SaveMemory(filename string, mem *AgentMemory) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return gob.NewEncoder(file).Encode(mem)
}

func main() {
	// Setup
	pq := &PriorityQueue{}
	heap.Init(pq)
	sw := NewSlidingWindow(3)

	// Add items with auto-vectorization
	rawItems := []struct {
		content  string
		priority int
	}{
		{"System: You are a helpful assistant.", 10},
		{"User: How do I fix the database connection?", 5},
		{"CRITICAL: Fix database connection leak.", 9},
	}

	for _, ri := range rawItems {
		item := &MemoryItem{
			Content:  ri.content,
			Priority: ri.priority,
			Vector:   TextToVector(ri.content),
		}
		heap.Push(pq, item)
		sw.Add(item)
	}

	// Sliding Window demonstration
	fmt.Println("--- Sliding Window (capacity 3) ---")
	for e := sw.queue.Front(); e != nil; e = e.Next() {
		fmt.Printf("  Window holds: '%s'\n", e.Value.(*MemoryItem).Content)
	}
	for i := 0; i < 2; i++ {
		sw.Add(&MemoryItem{
			ID:       i + 1,
			Content:  fmt.Sprintf("Stream item %d: recent event", i+1),
			Priority: 1,
			Vector:   TextToVector("recent event"),
		})
	}
	fmt.Println("  After 2 more stream items added:")
	for e := sw.queue.Front(); e != nil; e = e.Next() {
		fmt.Printf("  Window holds: '%s'\n", e.Value.(*MemoryItem).Content)
	}
	fmt.Println()

	// Semantic Search with Natural Language Query
	query := "database connection fix"
	queryVec := TextToVector(query)

	var bestMatch *MemoryItem
	maxSim := -1.0

	fmt.Printf("Querying for: '%s'\n", query)
	for _, item := range *pq {
		sim := CosineSimilarity(item.Vector, queryVec)
		fmt.Printf("Comparing with: '%s' (Sim: %.2f)\n", item.Content, sim)
		if sim > maxSim {
			maxSim = sim
			bestMatch = item
		}
	}

	fmt.Printf("\nBest match found: '%s' (Sim: %.2f)\n", bestMatch.Content, maxSim)

	// Persistence
	mem := &AgentMemory{LongTerm: *pq}
	SaveMemory("agent_memory.gob", mem)
	fmt.Println("Memory state saved to agent_memory.gob")
}
