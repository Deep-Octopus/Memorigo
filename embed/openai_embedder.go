package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OpenAIEmbedder struct {
	config    Config
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	dimension int
}

type OpenAIEmbeddingRequest struct {
	Input          interface{} `json:"input"` // string or []string
	Model          string      `json:"model"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     int         `json:"dimensions,omitempty"`
	User           string      `json:"user,omitempty"`
}

type OpenAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func NewOpenAIEmbedder(config Config) *OpenAIEmbedder {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	model := config.Model
	if model == "" {
		model = "text-embedding-ada-002"
	}

	// text-embedding-ada-002 has 1536 dimensions
	dimension := config.Dimension
	if dimension == 0 {
		dimension = 1536
	}

	return &OpenAIEmbedder{
		config:    config,
		client:    &http.Client{Timeout: 30 * time.Second},
		baseURL:   baseURL,
		apiKey:    config.APIKey,
		model:     model,
		dimension: dimension,
	}
}

func (e *OpenAIEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return make([]float32, e.dimension), nil
	}

	req := OpenAIEmbeddingRequest{
		Input: text,
		Model: e.model,
	}

	resp, err := e.makeRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data returned")
	}

	return resp.Data[0].Embedding, nil
}

func (e *OpenAIEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Filter out empty texts
	var filtered []string
	for _, text := range texts {
		if text != "" {
			filtered = append(filtered, text)
		}
	}

	if len(filtered) == 0 {
		result := make([][]float32, len(texts))
		for i := range result {
			result[i] = make([]float32, e.dimension)
		}
		return result, nil
	}

	req := OpenAIEmbeddingRequest{
		Input: filtered,
		Model: e.model,
	}

	resp, err := e.makeRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Data) != len(filtered) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(filtered), len(resp.Data))
	}

	result := make([][]float32, len(texts))
	filteredIndex := 0
	for i, text := range texts {
		if text == "" {
			result[i] = make([]float32, e.dimension)
		} else {
			result[i] = resp.Data[filteredIndex].Embedding
			filteredIndex++
		}
	}

	return result, nil
}

func (e *OpenAIEmbedder) makeRequest(ctx context.Context, req OpenAIEmbeddingRequest) (*OpenAIEmbeddingResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var embeddingResp OpenAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embeddingResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &embeddingResp, nil
}

func (e *OpenAIEmbedder) Dimension() int {
	return e.dimension
}

func (e *OpenAIEmbedder) Provider() string {
	return "openai"
}
