package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxResponseBytes = 8 << 20 // 8 MiB
	maxRetries       = 2
)

// postJSON sends a JSON payload with bounded retries on 429/5xx. The request
// body is recreated for every attempt, and retries honor Retry-After.
func postJSON(ctx context.Context, client *http.Client, url, apiKey string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "github.com/Deep-Octopus/Memorigo/1.0")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt < maxRetries && ctx.Err() == nil {
				sleepBackoff(ctx, attempt, 0)
				continue
			}
			return lastErr
		}

		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			return nil
		}

		lastErr = fmt.Errorf("embedding api http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
		if isRetryableStatus(resp.StatusCode) && attempt < maxRetries {
			retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			sleepBackoff(ctx, attempt, time.Duration(retryAfter)*time.Second)
			continue
		}
		return lastErr
	}
	return lastErr
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func embeddingEndpoint(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/embeddings"
	}
	return base + "/v1/embeddings"
}

func sleepBackoff(ctx context.Context, attempt int, hint time.Duration) {
	delay := hint
	if delay <= 0 {
		delay = 250 * time.Millisecond * time.Duration(1<<attempt)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// orderedEmbeddings maps an embedding API response onto the request order.
// OpenAI-compatible providers are allowed to return data out of order; the
// index field is authoritative.
func orderedEmbeddings(data []embeddingItem, want int) ([][]float32, error) {
	if len(data) != want {
		return nil, fmt.Errorf("expected %d embeddings, got %d", want, len(data))
	}
	out := make([][]float32, want)
	for _, d := range data {
		if d.Index < 0 || d.Index >= want {
			return nil, fmt.Errorf("embedding index %d out of range", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
