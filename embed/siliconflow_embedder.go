package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type SiliconFlowEmbedder struct {
	config    Config
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	dimension int
}

type SiliconFlowEmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type SiliconFlowEmbeddingResponse struct {
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

func NewSiliconFlowEmbedder(config Config) *SiliconFlowEmbedder {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("SILICONFLOW_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.siliconflow.cn"
		}
	}

	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("SILICONFLOW_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = "BAAI/bge-large-zh-v1.5" // 硅基流动支持的中文嵌入模型
	}

	// BAAI/bge-large-zh-v1.5 has 1024 dimensions
	dimension := config.Dimension
	if dimension == 0 {
		dimension = 1024
	}

	return &SiliconFlowEmbedder{
		config:    config,
		client:    &http.Client{Timeout: 30 * time.Second},
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
	}
}

func (e *SiliconFlowEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return make([]float32, e.dimension), nil
	}

	req := SiliconFlowEmbeddingRequest{
		Model: e.model,
		Input: text,
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

func (e *SiliconFlowEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := e.EmbedText(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text at index %d: %w", i, err)
		}
		result[i] = embedding
	}

	return result, nil
}

func (e *SiliconFlowEmbedder) makeRequest(ctx context.Context, req SiliconFlowEmbeddingRequest) (*SiliconFlowEmbeddingResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := e.baseURL + "/v1/embeddings"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
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

	var embeddingResp SiliconFlowEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embeddingResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &embeddingResp, nil
}

func (e *SiliconFlowEmbedder) Dimension() int {
	return e.dimension
}

func (e *SiliconFlowEmbedder) Provider() string {
	return "siliconflow"
}
