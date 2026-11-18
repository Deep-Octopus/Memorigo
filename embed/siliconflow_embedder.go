package embed

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const defaultSiliconFlowModel = "BAAI/bge-m3"

var siliconFlowModelDimensions = map[string]int{
	"BAAI/bge-m3":               1024,
	"BAAI/bge-large-zh-v1.5":    1024,
	"BAAI/bge-large-en-v1.5":    1024,
	"Qwen/Qwen3-Embedding-8B":   4096,
	"Qwen/Qwen3-Embedding-4B":   2560,
	"Qwen/Qwen3-Embedding-0.6B": 1024,
}

type SiliconFlowEmbedder struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	dimension atomic.Int64
}

type siliconFlowEmbeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type siliconFlowEmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func NewSiliconFlowEmbedder(config Config) *SiliconFlowEmbedder {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(os.Getenv("SILICONFLOW_BASE_URL"), "/")
	}
	if baseURL == "" {
		baseURL = "https://api.siliconflow.cn"
	}

	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("SILICONFLOW_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = defaultSiliconFlowModel
	}

	dimension := config.Dimension
	if dimension <= 0 {
		if d, ok := siliconFlowModelDimensions[model]; ok {
			dimension = d
		} else {
			dimension = siliconFlowModelDimensions[defaultSiliconFlowModel]
		}
	}

	e := &SiliconFlowEmbedder{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
	e.dimension.Store(int64(dimension))
	return e
}

func (e *SiliconFlowEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return make([]float32, e.Dimension()), nil
	}
	req := siliconFlowEmbeddingRequest{Model: e.model, Input: text}
	var resp siliconFlowEmbeddingResponse
	if err := postJSON(ctx, e.client, embeddingEndpoint(e.baseURL), e.apiKey, req, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embedding api returned no data")
	}
	if got := len(resp.Data[0].Embedding); got > 0 {
		e.dimension.Store(int64(got))
	}
	return resp.Data[0].Embedding, nil
}

func (e *SiliconFlowEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
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

	const batchSize = 64
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]
		req := siliconFlowEmbeddingRequest{Model: e.model, Input: batch}
		var resp siliconFlowEmbeddingResponse
		if err := postJSON(ctx, e.client, embeddingEndpoint(e.baseURL), e.apiKey, req, &resp); err != nil {
			return nil, err
		}
		vectors, err := orderedEmbeddings(resp.Data, len(batch))
		if err != nil {
			return nil, err
		}
		for j, vec := range vectors {
			if len(vec) == 0 {
				return nil, fmt.Errorf("embedding api returned an empty vector")
			}
			e.dimension.Store(int64(len(vec)))
			out[idx[start+j]] = vec
		}
	}
	return out, nil
}

func (e *SiliconFlowEmbedder) Dimension() int { return int(e.dimension.Load()) }

func (e *SiliconFlowEmbedder) Provider() string { return "siliconflow" }

func (e *SiliconFlowEmbedder) Model() string { return e.model }
