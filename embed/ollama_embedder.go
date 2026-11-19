package embed

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultOllamaBase  = "http://127.0.0.1:11434"
	defaultOllamaModel = "nomic-embed-text"
	defaultOllamaDim   = 768
)

// ollamaModelDimensions covers common local embedding models. An unknown model
// uses defaultOllamaDim until the first response reports its real length.
var ollamaModelDimensions = map[string]int{
	"nomic-embed-text":  768,
	"mxbai-embed-large": 1024,
	"bge-m3":            1024,
	"all-minilm":        384,
}

// OllamaEmbedder calls a local Ollama server at POST /api/embed. It does not
// download or run a model itself. Pin MEMORI_EMBEDDING_MODEL to the tag you
// pulled (for example nomic-embed-text) so vectors stay in one space.
type OllamaEmbedder struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	dimension atomic.Int64
	// configuredDim is non-zero when the caller fixed the dimension. A response
	// of a different length is rejected instead of silently mixing spaces.
	configuredDim int
}

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// NewOllamaEmbedder builds a client for Ollama's native embed endpoint.
func NewOllamaEmbedder(config Config) *OllamaEmbedder {
	model := config.Model
	if model == "" {
		model = defaultOllamaModel
	}
	dimension := config.Dimension
	if dimension <= 0 {
		if d, ok := ollamaModelDimensions[model]; ok {
			dimension = d
		} else {
			dimension = defaultOllamaDim
		}
	}
	e := &OllamaEmbedder{
		client:        &http.Client{Timeout: 60 * time.Second},
		baseURL:       ollamaRoot(config.BaseURL),
		apiKey:        config.APIKey,
		model:         model,
		configuredDim: config.Dimension,
	}
	e.dimension.Store(int64(dimension))
	return e
}

func (e *OllamaEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.EmbedTexts(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("ollama returned no embedding")
	}
	return vectors[0], nil
}

func (e *OllamaEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	idx := make([]int, 0, len(texts))
	inputs := make([]string, 0, len(texts))
	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			out[i] = make([]float32, e.Dimension())
			continue
		}
		idx = append(idx, i)
		inputs = append(inputs, text)
	}
	if len(inputs) == 0 {
		return out, nil
	}

	const batchSize = 32
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]
		var resp ollamaEmbedResponse
		err := postJSON(ctx, e.client, e.baseURL+"/api/embed", e.apiKey, ollamaEmbedRequest{
			Model: e.model,
			Input: batch,
		}, &resp)
		if err != nil {
			return nil, err
		}
		if len(resp.Embeddings) != len(batch) {
			return nil, fmt.Errorf("expected %d embeddings, got %d", len(batch), len(resp.Embeddings))
		}
		for j, vec := range resp.Embeddings {
			if len(vec) == 0 {
				return nil, fmt.Errorf("ollama returned an empty vector")
			}
			if e.configuredDim > 0 && len(vec) != e.configuredDim {
				return nil, fmt.Errorf("%w: configured %d, ollama returned %d", ErrEmbeddingDimensionMismatch, e.configuredDim, len(vec))
			}
			e.dimension.Store(int64(len(vec)))
			out[idx[start+j]] = vec
		}
	}
	return out, nil
}

func (e *OllamaEmbedder) Dimension() int { return int(e.dimension.Load()) }

func (e *OllamaEmbedder) Provider() string { return "ollama" }

func (e *OllamaEmbedder) Model() string { return e.model }

func ollamaRoot(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, "/v1")
	base = strings.TrimRight(base, "/")
	if base == "" {
		return defaultOllamaBase
	}
	return base
}
