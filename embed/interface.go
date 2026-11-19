package embed

import (
	"context"
	"errors"
	"math"
	"strings"
)

var (
	ErrEmbeddingServiceUnavailable = errors.New("embedding service unavailable")
	ErrEmbeddingDimensionMismatch  = errors.New("embedding dimension mismatch")
)

// Embedder is the provider-neutral embedding contract. Implementations must be
// safe for concurrent use.
type Embedder interface {
	// EmbedText converts a single text into a vector.
	EmbedText(ctx context.Context, text string) ([]float32, error)

	// EmbedTexts converts a batch of texts into vectors, preserving input order.
	EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the vector dimension produced by this embedder.
	Dimension() int

	// Provider returns the provider name, e.g. "openai" or "hash".
	Provider() string

	// Model returns the concrete model identifier, e.g. "text-embedding-3-small".
	Model() string
}

// Config configures an embedder.
type Config struct {
	Provider  string // "openai", "siliconflow", "ollama", "hash" (offline fallback)
	APIKey    string
	BaseURL   string
	Model     string
	Dimension int
	// CacheSize enables an in-process LRU embedding cache when > 0.
	CacheSize int
}

// NewEmbedder builds an embedder from config and wraps it in an LRU cache when
// CacheSize is positive.
func NewEmbedder(config Config) Embedder {
	var e Embedder
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "openai":
		e = NewOpenAIEmbedder(config)
	case "siliconflow":
		e = NewSiliconFlowEmbedder(config)
	case "ollama":
		e = NewOllamaEmbedder(config)
	case "hash":
		fallthrough
	default:
		dim := config.Dimension
		if dim <= 0 {
			dim = 256
		}
		e = NewHashEmbedderWithDim(dim)
	}
	if config.CacheSize > 0 {
		return NewCachedEmbedder(e, config.CacheSize)
	}
	return e
}

// CosineSimilarity computes cosine similarity in [-1, 1]. It returns 0 for
// empty vectors or dimension mismatches.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if sim > 1 {
		return 1
	}
	if sim < -1 {
		return -1
	}
	return sim
}
