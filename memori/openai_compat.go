package memori

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	chatMaxResponseBytes = 32 << 20
	chatMaxErrorBytes    = 8 << 10
	chatDefaultRetries   = 2
)

type OpenAICompatOptions struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
	Headers    map[string]string
}

// OpenAICompatClient talks to any OpenAI-compatible Chat Completions endpoint.
type OpenAICompatClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
	Headers    map[string]string
}

func NewOpenAICompatClient(opts OpenAICompatOptions) *OpenAICompatClient {
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	c := opts.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 120 * time.Second}
	}
	retries := opts.MaxRetries
	if retries < 0 {
		retries = 0
	}
	if retries == 0 {
		retries = chatDefaultRetries
	}
	return &OpenAICompatClient{
		BaseURL:    base,
		APIKey:     opts.APIKey,
		HTTPClient: c,
		MaxRetries: retries,
		Headers:    opts.Headers,
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatCompletionsRequest supports the common subset plus Extra for
// provider-specific parameters (e.g. top_k, reasoning_effort). Extra keys never
// override explicitly set fields.
type ChatCompletionsRequest struct {
	Model          string         `json:"model"`
	Messages       []ChatMessage  `json:"messages"`
	Stream         bool           `json:"stream,omitempty"`
	Temperature    *float64       `json:"temperature,omitempty"`
	TopP           *float64       `json:"top_p,omitempty"`
	MaxTokens      *int           `json:"max_tokens,omitempty"`
	Stop           any            `json:"stop,omitempty"`
	ResponseFormat any            `json:"response_format,omitempty"`
	Extra          map[string]any `json:"-"`
}

func (r ChatCompletionsRequest) MarshalJSON() ([]byte, error) {
	type alias ChatCompletionsRequest
	raw, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		if _, exists := obj[k]; exists {
			continue
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		obj[k] = encoded
	}
	return json.Marshal(obj)
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	Delta        ChatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionsResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

func (c *OpenAICompatClient) endpoint() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func (c *OpenAICompatClient) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "github.com/Deep-Octopus/Memorigo/1.0")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// doPost sends a chat request, retrying only before any response bytes are
// consumed. Once a stream starts, retrying would duplicate output.
func (c *OpenAICompatClient) doPost(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		req, err := c.newRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.MaxRetries && ctx.Err() == nil {
				sleepChatBackoff(ctx, attempt, 0)
				continue
			}
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		data, _ := io.ReadAll(io.LimitReader(resp.Body, chatMaxErrorBytes))
		resp.Body.Close()
		lastErr = fmt.Errorf("openai_compat http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
		if isRetryableChatStatus(resp.StatusCode) && attempt < c.MaxRetries {
			retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			sleepChatBackoff(ctx, attempt, time.Duration(retryAfter)*time.Second)
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func isRetryableChatStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func sleepChatBackoff(ctx context.Context, attempt int, hint time.Duration) {
	delay := hint
	if delay <= 0 {
		delay = 300 * time.Millisecond * time.Duration(1<<attempt)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (c *OpenAICompatClient) ChatCompletionsCreate(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	var out ChatCompletionsResponse
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	resp, err := c.doPost(ctx, body, false)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, chatMaxResponseBytes))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode chat response: %w", err)
	}
	return out, nil
}

type StreamEvent struct {
	RawLine string
	Chunk   *ChatCompletionsResponse
	Done    bool
}

// ChatCompletionsStream implements the SSE "data: {json}" streaming protocol
// used by OpenAI-compatible providers. It stops at [DONE] or context
// cancellation.
func (c *OpenAICompatClient) ChatCompletionsStream(ctx context.Context, req ChatCompletionsRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 128)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		req.Stream = true
		body, err := json.Marshal(req)
		if err != nil {
			sendStreamError(errs, err)
			return
		}
		resp, err := c.doPost(ctx, body, true)
		if err != nil {
			sendStreamError(errs, err)
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if raw == "" {
				continue
			}
			if raw == "[DONE]" {
				sendStreamEvent(ctx, events, StreamEvent{RawLine: raw, Done: true})
				return
			}
			var chunk ChatCompletionsResponse
			if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
				if !sendStreamEvent(ctx, events, StreamEvent{RawLine: raw}) {
					return
				}
				continue
			}
			if !sendStreamEvent(ctx, events, StreamEvent{RawLine: raw, Chunk: &chunk}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				sendStreamError(errs, err)
			}
		}
	}()

	return events, errs
}

func sendStreamEvent(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendStreamError(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}
