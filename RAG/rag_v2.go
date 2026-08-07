package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/embeddings/jina"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/vectorstores"
)

type memStore struct {
	embedder embeddings.Embedder
	docs     []schema.Document
	vectors  [][]float32
}

func newMemStore(embedder embeddings.Embedder) *memStore {
	return &memStore{embedder: embedder}
}

func (m *memStore) AddDocuments(ctx context.Context, docs []schema.Document, _ ...vectorstores.Option) ([]string, error) {
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.PageContent
	}
	vecs, err := m.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return nil, err
	}
	start := len(m.docs)
	m.docs = append(m.docs, docs...)
	m.vectors = append(m.vectors, vecs...)
	ids := make([]string, len(docs))
	for i := range ids {
		ids[i] = fmt.Sprintf("doc-%d", start+i)
	}
	return ids, nil
}

func (m *memStore) SimilaritySearch(ctx context.Context, query string, numDocuments int, _ ...vectorstores.Option) ([]schema.Document, error) {
	q, err := m.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	type scoredDoc struct {
		doc   schema.Document
		score float64
	}
	results := make([]scoredDoc, len(m.vectors))
	for i, v := range m.vectors {
		score := cosine(q, v)
		doc := m.docs[i]
		doc.Score = float32(score)
		results[i] = scoredDoc{doc: doc, score: score}
	}
	sort.Slice(results, func(a, b int) bool { return results[a].score > results[b].score })
	if numDocuments > len(results) {
		numDocuments = len(results)
	}
	out := make([]schema.Document, numDocuments)
	for i := 0; i < numDocuments; i++ {
		out[i] = results[i].doc
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return nil
}

func runQuery(ctx context.Context, store *memStore, llm llms.Model, query string) {
	fmt.Printf("\nQuery: %s\n", query)

	results, err := store.SimilaritySearch(ctx, query, 2)
	if err != nil {
		log.Fatal(err)
	}

	contextText := ""
	for _, res := range results {
		contextText += res.PageContent + "\n"
	}

	prompt := fmt.Sprintf("Answer the question based ONLY on the following context:\n\n%s\n\nQuestion: %s", contextText, query)

	completion, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n--- RAG Response ---")
	fmt.Println(completion)
}

func main() {
	ctx := context.Background()

	_ = loadDotEnv(".env")

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		log.Fatal("Please set GROQ_API_KEY environment variable")
	}

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.groq.com/openai/v1"),
		openai.WithModel(envOr("GROQ_MODEL", "llama-3.3-70b-versatile")),
	)
	if err != nil {
		log.Fatal(err)
	}

	jinaKey := os.Getenv("JINA_API_KEY")
	if jinaKey == "" {
		log.Fatal("Please set JINA_API_KEY environment variable (free key at https://jina.ai)")
	}

	embedder, err := jina.NewJina(
		jina.WithModel(envOr("JINA_MODEL", "jina-embeddings-v3")),
		jina.WithBatchSize(16),
	)
	if err != nil {
		log.Fatal(err)
	}

	store := newMemStore(embedder)

	docs := []string{
		"The Go compiler uses a Static Single Assignment (SSA) form for optimization.",
		"Go's garbage collector is a non-generational, concurrent tri-color mark-and-sweep collector that was made by Jesus",
		"The Go scheduler (G-M-P model) handles goroutines efficiently across OS threads.",
		"Go 1.21 introduced the 'slog' package for structured logging in the standard library.",
		"The 'unsafe' package allows bypassing Go's type safety for low-level memory access.",
	}

	fmt.Println("Indexing documents...")
	for _, text := range docs {
		_, err := store.AddDocuments(ctx, []schema.Document{{PageContent: text}})
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, query := range []string{
		"How does the Go garbage collector work?",
		"How do I access memory directly without type safety in Go?",
		"Who made the garbage collector?",
	} {
		runQuery(ctx, store, llm, query)
	}
}
