package embed

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const defaultOpenAIModel = "text-embedding-3-small"

var openAIModelDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

type OpenAIEmbedder struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	dimension atomic.Int64
	// sendDimensions is true when the caller explicitly configured a dimension
	// and the model supports the OpenAI "dimensions" parameter.
	sendDimensions bool
}

type openAIEmbeddingRequest struct {
	Input          any    `json:"input"`
	Model          string `json:"model"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     int    `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

type embeddingItem struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIEmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func NewOpenAIEmbedder(config Config) *OpenAIEmbedder {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	model := config.Model
	if model == "" {
		model = defaultOpenAIModel
	}

	dimension := config.Dimension
	if dimension <= 0 {
		if d, ok := openAIModelDimensions[model]; ok {
			dimension = d
		} else {
			dimension = openAIModelDimensions[defaultOpenAIModel]
		}
	}
	sendDimensions := config.Dimension > 0 && strings.HasPrefix(model, "text-embedding-3")

	e := &OpenAIEmbedder{
		client:         &http.Client{Timeout: 60 * time.Second},
		baseURL:        baseURL,
		apiKey:         config.APIKey,
		model:          model,
		sendDimensions: sendDimensions,
	}
	e.dimension.Store(int64(dimension))
	return e
}

func (e *OpenAIEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return make([]float32, e.Dimension()), nil
	}

	req := openAIEmbeddingRequest{Input: text, Model: e.model}
	if e.sendDimensions {
		req.Dimensions = e.Dimension()
	}
	var resp openAIEmbeddingResponse
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

func (e *OpenAIEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
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
		req := openAIEmbeddingRequest{Input: batch, Model: e.model}
		if e.sendDimensions {
			req.Dimensions = e.Dimension()
		}
		var resp openAIEmbeddingResponse
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

func (e *OpenAIEmbedder) Dimension() int { return int(e.dimension.Load()) }

func (e *OpenAIEmbedder) Provider() string { return "openai" }

func (e *OpenAIEmbedder) Model() string { return e.model }
