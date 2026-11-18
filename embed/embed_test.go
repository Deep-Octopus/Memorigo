package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	if got := CosineSimilarity([]float32{1, 0}, []float32{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Fatalf("identical = %v", got)
	}
	if got := CosineSimilarity([]float32{1, 0}, []float32{0, 1}); math.Abs(got) > 1e-9 {
		t.Fatalf("orthogonal = %v", got)
	}
	if got := CosineSimilarity([]float32{1, 0}, []float32{-1, 0}); math.Abs(got+1) > 1e-9 {
		t.Fatalf("opposite = %v", got)
	}
	if got := CosineSimilarity([]float32{1, 0}, []float32{1, 0, 0}); got != 0 {
		t.Fatalf("dimension mismatch = %v", got)
	}
	if got := CosineSimilarity(nil, nil); got != 0 {
		t.Fatalf("empty = %v", got)
	}
}

func TestHashEmbedderDeterministicAndNormalized(t *testing.T) {
	e := NewHashEmbedderWithDim(128)
	ctx := context.Background()
	a1, err := e.EmbedText(ctx, "I love programming in Go")
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := e.EmbedText(ctx, "I love programming in Go")
	if CosineSimilarity(a1, a2) < 0.9999 {
		t.Fatal("hash embedding is not deterministic")
	}
	if len(a1) != 128 {
		t.Fatalf("dimension = %d", len(a1))
	}
	var norm float64
	for _, v := range a1 {
		norm += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-5 {
		t.Fatalf("embedding not L2-normalized: %v", math.Sqrt(norm))
	}
	if e.Provider() != "hash" || e.Model() == "" || e.Dimension() != 128 {
		t.Fatalf("metadata mismatch: %s/%s/%d", e.Provider(), e.Model(), e.Dimension())
	}
}

func TestHashEmbedderRelatedCloserThanUnrelated(t *testing.T) {
	e := NewHashEmbedder()
	ctx := context.Background()
	base, _ := e.EmbedText(ctx, "I love programming in Go")
	related, _ := e.EmbedText(ctx, "My favorite programming language is Go")
	unrelated, _ := e.EmbedText(ctx, "The weather is nice today")

	if CosineSimilarity(base, related) <= CosineSimilarity(base, unrelated) {
		t.Fatalf("related (%v) should be closer than unrelated (%v)",
			CosineSimilarity(base, related), CosineSimilarity(base, unrelated))
	}
}

type countingEmbedder struct {
	calls atomic.Int64
}

func (c *countingEmbedder) EmbedText(context.Context, string) ([]float32, error) {
	c.calls.Add(1)
	return []float32{1}, nil
}
func (c *countingEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i], _ = c.EmbedText(ctx, texts[i])
	}
	return out, nil
}
func (c *countingEmbedder) Dimension() int   { return 1 }
func (c *countingEmbedder) Provider() string { return "counting" }
func (c *countingEmbedder) Model() string    { return "counting-v1" }

func TestCachedEmbedderAvoidsRepeatedCalls(t *testing.T) {
	inner := &countingEmbedder{}
	cached := NewCachedEmbedder(inner, 2)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := cached.EmbedText(ctx, "same"); err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls.Load())
	}

	// Batch path should reuse cache and only embed misses.
	_, err := cached.EmbedTexts(ctx, []string{"same", "new"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("inner calls after batch = %d, want 2", inner.calls.Load())
	}
}

func TestOpenAIEmbedderBatchPreservesOrderAndDimensions(t *testing.T) {
	var sawDimensions bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if _, ok := req["dimensions"]; ok {
			sawDimensions = true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0, 1}},
				{"index": 0, "embedding": []float32{1, 0}},
			},
		})
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(Config{
		BaseURL:   srv.URL,
		APIKey:    "test",
		Model:     "text-embedding-3-small",
		Dimension: 2,
	})
	vectors, err := e.EmbedTexts(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("order not preserved: %#v", vectors)
	}
	if !sawDimensions {
		t.Fatal("explicit dimension should be sent for text-embedding-3 models")
	}
}

func TestOpenAIEmbedderRetriesRateLimit(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float32{1, 0}}},
		})
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(Config{BaseURL: srv.URL + "/v1", Model: "text-embedding-3-small"})
	vec, err := e.EmbedText(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if len(vec) != 2 {
		t.Fatalf("vector = %#v", vec)
	}
}

func TestAPIEmbeddersConcurrentUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		n := 1
		if batch, ok := req.Input.([]any); ok {
			n = len(batch)
		}
		data := make([]map[string]any, n)
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []float32{0.6, 0.8}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	for _, e := range []Embedder{
		NewOpenAIEmbedder(Config{BaseURL: srv.URL, APIKey: "test", Model: "text-embedding-3-small"}),
		NewSiliconFlowEmbedder(Config{BaseURL: srv.URL, APIKey: "test"}),
	} {
		t.Run(e.Provider(), func(t *testing.T) {
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 4; j++ {
						vec, err := e.EmbedText(context.Background(), "hello")
						if err != nil || len(vec) != 2 {
							t.Errorf("EmbedText = %v, %v", vec, err)
							return
						}
						vecs, err := e.EmbedTexts(context.Background(), []string{"a", "b"})
						if err != nil || len(vecs) != 2 || len(vecs[0]) != 2 {
							t.Errorf("EmbedTexts = %v, %v", vecs, err)
							return
						}
						if e.Dimension() != 2 {
							t.Errorf("Dimension = %d, want 2", e.Dimension())
							return
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

func TestNewEmbedderFallsBackToHash(t *testing.T) {
	e := NewEmbedder(Config{Provider: "unknown"})
	if e.Provider() != "hash" {
		t.Fatalf("provider = %s", e.Provider())
	}
}
