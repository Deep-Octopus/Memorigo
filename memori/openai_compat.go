package memori

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatOptions struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type OpenAICompatClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewOpenAICompatClient(opts OpenAICompatOptions) *OpenAICompatClient {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	c := opts.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAICompatClient{
		BaseURL:    base,
		APIKey:     opts.APIKey,
		HTTPClient: c,
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

// OpenAI-compatible (subset) response
type ChatCompletionsResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (c *OpenAICompatClient) ChatCompletionsCreate(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	var out ChatCompletionsResponse
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return out, fmt.Errorf("openai_compat http %d: %s", resp.StatusCode, string(b))
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

type StreamEvent struct {
	RawLine string
	Chunk   *ChatCompletionsResponse
	Done    bool
}

// ChatCompletionsStream implements SSE "data: {json}" streaming used by OpenAI-compatible providers.
// It returns a channel that yields chunks until [DONE] or context cancellation.
func (c *OpenAICompatClient) ChatCompletionsStream(ctx context.Context, req ChatCompletionsRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 128)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		req.Stream = true
		body, err := json.Marshal(req)
		if err != nil {
			errs <- err
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		}

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			errs <- fmt.Errorf("openai_compat http %d: %s", resp.StatusCode, string(b))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			// SSE can include empty lines / comments
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if raw == "" {
				continue
			}
			if raw == "[DONE]" {
				events <- StreamEvent{RawLine: raw, Done: true}
				return
			}
			var chunk ChatCompletionsResponse
			if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
				events <- StreamEvent{RawLine: raw, Chunk: nil}
				continue
			}
			events <- StreamEvent{RawLine: raw, Chunk: &chunk}
		}
		if err := scanner.Err(); err != nil {
			errs <- err
			return
		}
	}()

	return events, errs
}


