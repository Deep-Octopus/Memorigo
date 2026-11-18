package memori_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/storage"
)

type chatRecorder struct {
	mu       sync.Mutex
	requests []memori.ChatCompletionsRequest
}

func (r *chatRecorder) record(req memori.ChatCompletionsRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

func (r *chatRecorder) all() []memori.ChatCompletionsRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]memori.ChatCompletionsRequest, len(r.requests))
	copy(out, r.requests)
	return out
}

func (r *chatRecorder) waitFor(t *testing.T, n int) []memori.ChatCompletionsRequest {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reqs := r.all()
		if len(reqs) >= n {
			return reqs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d requests, got %d", n, len(reqs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeChatResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(memori.ChatCompletionsResponse{
		ID:     "chatcmpl-test",
		Object: "chat.completion",
		Model:  "test-model",
		Choices: []memori.ChatChoice{{
			Index:        0,
			Message:      memori.ChatMessage{Role: "assistant", Content: content},
			FinishReason: "stop",
		}},
	})
}

// registerMockClient starts a mock Chat Completions endpoint, records every
// request, and wires it into m.
func registerMockClient(t *testing.T, m *memori.Memori, handler func(w http.ResponseWriter, req memori.ChatCompletionsRequest)) *chatRecorder {
	t.Helper()
	rec := &chatRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req memori.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.record(req)
		handler(w, req)
	}))
	t.Cleanup(srv.Close)
	m.OpenAI.Register(memori.NewOpenAICompatClient(memori.OpenAICompatOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		MaxRetries: 1,
	}))
	return rec
}

func waitForStoredMessages(t *testing.T, m *memori.Memori, want int) []storage.StoredMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		msgs := recentStoredMessages(t, m)
		if len(msgs) >= want {
			return msgs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d stored messages, got %d", want, len(msgs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAutoInjectAndOriginalPersistence(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}

	rec := registerMockClient(t, m, func(w http.ResponseWriter, _ memori.ChatCompletionsRequest) {
		writeChatResponse(w, "Blue.")
	})

	resp, err := m.OpenAIClient().ChatCompletionsCreate(context.Background(), memori.ChatCompletionsRequest{
		Model:    "test-model",
		Messages: []memori.ChatMessage{{Role: "user", Content: "What is my favorite color?"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "Blue." {
		t.Fatalf("response = %#v", resp)
	}

	reqs := rec.waitFor(t, 1)
	var injected bool
	for _, msg := range reqs[0].Messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "<memories>") && strings.Contains(msg.Content, "blue") {
			injected = true
		}
	}
	if !injected {
		t.Fatalf("memory system message missing: %#v", reqs[0].Messages)
	}
	if last := reqs[0].Messages[len(reqs[0].Messages)-1]; last.Role != "user" || last.Content != "What is my favorite color?" {
		t.Fatalf("user message moved or altered: %#v", reqs[0].Messages)
	}

	msgs := waitForStoredMessages(t, m, 2)
	if len(msgs) != 2 {
		t.Fatalf("stored messages = %d", len(msgs))
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "<memories>") || strings.Contains(msg.Content, "memorigo:memory") {
			t.Fatalf("injected memory was persisted as history: %#v", msgs)
		}
	}
	if msgs[0].Content != "What is my favorite color?" || msgs[1].Content != "Blue." {
		t.Fatalf("stored messages = %#v", msgs)
	}
}

func TestAutoInjectCanBeDisabled(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	m.SetAutoInject(false)

	rec := registerMockClient(t, m, func(w http.ResponseWriter, _ memori.ChatCompletionsRequest) {
		writeChatResponse(w, "ok")
	})
	if _, err := m.OpenAIClient().ChatCompletionsCreate(context.Background(), memori.ChatCompletionsRequest{
		Model:    "test-model",
		Messages: []memori.ChatMessage{{Role: "user", Content: "What is my favorite color?"}},
	}); err != nil {
		t.Fatal(err)
	}

	reqs := rec.waitFor(t, 1)
	for _, msg := range reqs[0].Messages {
		if strings.Contains(msg.Content, "<memories>") {
			t.Fatalf("memory injected while disabled: %#v", reqs[0].Messages)
		}
	}
}

func TestAutoInjectSkipsWhenCallerProvidedMemory(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}

	rec := registerMockClient(t, m, func(w http.ResponseWriter, _ memori.ChatCompletionsRequest) {
		writeChatResponse(w, "ok")
	})
	if _, err := m.OpenAIClient().ChatCompletionsCreate(context.Background(), memori.ChatCompletionsRequest{
		Model: "test-model",
		Messages: []memori.ChatMessage{
			{Role: "system", Content: "<memories>custom</memories>"},
			{Role: "user", Content: "What is my favorite color?"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	reqs := rec.waitFor(t, 1)
	count := 0
	for _, msg := range reqs[0].Messages {
		if strings.Contains(msg.Content, "<memories>") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one memory block, got %d: %#v", count, reqs[0].Messages)
	}
}

func TestStreamingPersistsAssistantText(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	rec := registerMockClient(t, m, func(w http.ResponseWriter, _ memori.ChatCompletionsRequest) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("server does not support flushing")
			return
		}
		for _, tok := range []string{"Hel", "lo"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", tok)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})

	events, errs := m.OpenAIClient().ChatCompletionsStream(context.Background(), memori.ChatCompletionsRequest{
		Model:    "test-model",
		Messages: []memori.ChatMessage{{Role: "user", Content: "Say hello"}},
	})
	var b strings.Builder
	for ev := range events {
		if ev.Chunk != nil && len(ev.Chunk.Choices) > 0 {
			b.WriteString(ev.Chunk.Choices[0].Delta.Content)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	if b.String() != "Hello" {
		t.Fatalf("streamed text = %q", b.String())
	}

	reqs := rec.waitFor(t, 1)
	if !reqs[0].Stream {
		t.Fatal("upstream request was not marked as streaming")
	}

	msgs := waitForStoredMessages(t, m, 2)
	if msgs[0].Content != "Say hello" || msgs[1].Content != "Hello" {
		t.Fatalf("stored messages = %#v", msgs)
	}
}

func TestStreamingPropagatesUpstreamError(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	registerMockClient(t, m, func(w http.ResponseWriter, _ memori.ChatCompletionsRequest) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	})

	events, errs := m.OpenAIClient().ChatCompletionsStream(context.Background(), memori.ChatCompletionsRequest{
		Model:    "test-model",
		Messages: []memori.ChatMessage{{Role: "user", Content: "hello"}},
	})
	for range events {
	}
	var got error
	for err := range errs {
		if err != nil {
			got = err
		}
	}
	if got == nil || !strings.Contains(got.Error(), "500") {
		t.Fatalf("expected upstream error, got %v", got)
	}
}

func TestOpenAIClientAccessIsConcurrentSafe(t *testing.T) {
	m := newTestMemori(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.OpenAIClient()
			m.OpenAI.Register(memori.NewOpenAIClient())
			_ = m.OpenAI.Client()
		}()
	}
	wg.Wait()
	if m.OpenAI.Client() == nil {
		t.Fatal("expected a registered client")
	}
}

func TestLLMExtractionEndToEnd(t *testing.T) {
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_EXTRACTION_PROVIDER": "llm",
		"MEMORI_EXTRACTION_MODEL":    "mock-extractor",
	})
	_ = m.TryAttribution("user", "proc")

	var sawExtraction atomic.Bool
	registerMockClient(t, m, func(w http.ResponseWriter, req memori.ChatCompletionsRequest) {
		isExtraction := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "long-term memory extraction engine")
		if isExtraction {
			sawExtraction.Store(true)
			writeChatResponse(w, `{"facts":[{"content":"I love Go","slot":"","exclusive":false}],"summary":"User loves Go."}`)
			return
		}
		writeChatResponse(w, "Great!")
	})

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "I love Go"}},
		&memori.Message{Role: "assistant", Content: "Great!"},
	)
	flushMemori(t, m)

	if !sawExtraction.Load() {
		t.Fatal("LLM extractor was not called")
	}
	facts, err := m.Recall("Go", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || !strings.Contains(facts[0].Content, "Go") {
		t.Fatalf("facts = %#v", facts)
	}
	if summary := conversationSummary(t, m); !strings.Contains(summary, "User loves Go") {
		t.Fatalf("summary = %q", summary)
	}
}
