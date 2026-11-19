package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRerankerOrdersByReturnedIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Query != "seats" || len(req.Documents) != 2 || req.TopN != 2 || req.Model != "bge-reranker" {
			t.Fatalf("request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.9},
				{"index": 0, "score": 0.2},
			},
		})
	}))
	defer srv.Close()

	r := NewHTTPReranker(RerankConfig{URL: srv.URL, Model: "bge-reranker"})
	scores, err := r.Rerank(context.Background(), "seats", []string{"aisle", "window"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[1] != 0.9 || scores[0] != 0.2 {
		t.Fatalf("scores = %#v", scores)
	}
}

func TestHTTPRerankerLeavesOmittedDocumentsLast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"index": 0, "relevance_score": 0.4}},
		})
	}))
	defer srv.Close()

	scores, err := NewHTTPReranker(RerankConfig{URL: srv.URL}).Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if scores[0] != 0.4 || !math.IsInf(scores[1], -1) {
		t.Fatalf("scores = %#v", scores)
	}
}

func TestHTTPRerankerRequiresURL(t *testing.T) {
	_, err := NewHTTPReranker(RerankConfig{}).Rerank(context.Background(), "q", []string{"a"})
	if err == nil {
		t.Fatal("expected an empty-url error")
	}
}
