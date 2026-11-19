package embed

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

// Reranker scores documents against a query. Scores align with documents:
// a higher score is a better match. Implementations must be safe for
// concurrent use.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
}

// RerankConfig configures an HTTP reranker. URL is the full endpoint, for
// example https://api.jina.ai/v1/rerank. The request body is the common
// query/documents/top_n shape used by Jina and Cohere-compatible servers.
type RerankConfig struct {
	URL    string
	APIKey string
	Model  string
}

// HTTPReranker is an optional cross-encoder client. It is off unless the
// caller constructs one. Recall keeps its hybrid order when this call fails.
type HTTPReranker struct {
	client *http.Client
	url    string
	apiKey string
	model  string
}

type rerankRequest struct {
	Model     string   `json:"model,omitempty"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankHit struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Score          float64 `json:"score"`
}

func (h rerankHit) value() float64 {
	if h.RelevanceScore != 0 || h.Score == 0 {
		return h.RelevanceScore
	}
	return h.Score
}

type rerankResponse struct {
	Results []rerankHit `json:"results"`
}

// NewHTTPReranker builds a rerank client. An empty URL fails on Rerank rather
// than at construction, matching the embedding clients.
func NewHTTPReranker(cfg RerankConfig) *HTTPReranker {
	client := &http.Client{Timeout: 30 * time.Second}
	return &HTTPReranker{
		client: client,
		url:    strings.TrimSpace(cfg.URL),
		apiKey: cfg.APIKey,
		model:  cfg.Model,
	}
}

// Rerank returns one score per document. Documents the server omits receive
// negative infinity so they sort after every scored hit.
func (h *HTTPReranker) Rerank(ctx context.Context, query string, documents []string) ([]float64, error) {
	if h == nil {
		return nil, fmt.Errorf("reranker is nil")
	}
	if strings.TrimSpace(h.url) == "" {
		return nil, fmt.Errorf("rerank url is empty")
	}
	if len(documents) == 0 {
		return nil, nil
	}
	var resp rerankResponse
	err := postJSON(ctx, h.client, h.url, h.apiKey, rerankRequest{
		Model:     h.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	}, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("rerank api returned no results")
	}
	scores := make([]float64, len(documents))
	for i := range scores {
		scores[i] = math.Inf(-1)
	}
	seen := make(map[int]struct{}, len(resp.Results))
	for _, hit := range resp.Results {
		if hit.Index < 0 || hit.Index >= len(documents) {
			return nil, fmt.Errorf("rerank index %d out of range", hit.Index)
		}
		if _, ok := seen[hit.Index]; ok {
			return nil, fmt.Errorf("rerank duplicate index %d", hit.Index)
		}
		seen[hit.Index] = struct{}{}
		scores[hit.Index] = hit.value()
	}
	return scores, nil
}
