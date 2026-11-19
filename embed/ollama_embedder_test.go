package embed

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaEmbedderUsesNativeEndpoint(t *testing.T) {
	var got ollamaEmbedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer local-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{
			Model: "nomic-embed-text",
			Embeddings: [][]float32{
				{1, 0},
				{0, 1},
			},
		})
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(Config{
		BaseURL:   srv.URL + "/v1",
		APIKey:    "local-key",
		Model:     "nomic-embed-text",
		Dimension: 2,
	})
	if e.Provider() != "ollama" || e.Model() != "nomic-embed-text" || e.Dimension() != 2 {
		t.Fatalf("identity = %s %s %d", e.Provider(), e.Model(), e.Dimension())
	}
	vectors, err := e.EmbedTexts(context.Background(), []string{"alpha", " ", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "nomic-embed-text" {
		t.Fatalf("model = %q", got.Model)
	}
	inputs, ok := got.Input.([]any)
	if !ok || len(inputs) != 2 || inputs[0] != "alpha" || inputs[1] != "beta" {
		t.Fatalf("input = %#v", got.Input)
	}
	if len(vectors) != 3 || len(vectors[1]) != 2 || vectors[0][0] != 1 || vectors[2][1] != 1 {
		t.Fatalf("vectors = %#v", vectors)
	}
	for _, v := range vectors[1] {
		if v != 0 {
			t.Fatalf("blank text was embedded: %#v", vectors[1])
		}
	}
}

func TestOllamaEmbedderRejectsDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{
			Embeddings: [][]float32{{1, 0, 0}},
		})
	}))
	defer srv.Close()

	e := NewEmbedder(Config{Provider: "Ollama", BaseURL: srv.URL, Dimension: 2})
	_, err := e.EmbedText(context.Background(), "alpha")
	if !errors.Is(err, ErrEmbeddingDimensionMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestOllamaRootDefaults(t *testing.T) {
	if got := ollamaRoot(" http://127.0.0.1:11434/v1/ "); got != "http://127.0.0.1:11434" {
		t.Fatalf("root = %q", got)
	}
	if got := ollamaRoot(""); got != defaultOllamaBase {
		t.Fatalf("empty root = %q", got)
	}
}
