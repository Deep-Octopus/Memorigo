// Package adapter is the framework-neutral hook for putting Memorigo context
// into a chat request. It does not import Eino, Genkit, or any other agent
// framework. A framework middleware should call Apply, send Result.Messages,
// and persist the original messages rather than the injected copy.
package adapter

import (
	"context"
	"errors"
	"strings"

	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/textutil"
)

// Marker is appended to an injected system message so a second Apply, or the
// OpenAI-compatible auto-inject path, does not add another copy.
const Marker = "<!-- memorigo:memory -->"

// Options selects how much memory is packed in front of the request.
type Options struct {
	// Budget is the context token budget. Zero uses Memorigo's default.
	Budget int
	// FactLimit caps recalled facts. Zero uses Memorigo's default.
	FactLimit int
	// IncludeRaw adds raw conversation evidence inside the same budget.
	IncludeRaw bool
}

// Result is the request to send and the context that produced it.
type Result struct {
	Messages []memori.ChatMessage
	Context  memori.ContextPackage
	// Injected is false when there was nothing to add, or when a memory block
	// was already present. Messages is still a copy of the input.
	Injected bool
}

// Apply inserts one system message built from BuildContext. It does not write
// that message into memory. The input slice is left unchanged.
func Apply(ctx context.Context, m *memori.Memori, messages []memori.ChatMessage, opts Options) (Result, error) {
	if m == nil {
		return Result{}, errors.New("adapter: memori is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := Result{Messages: cloneMessages(messages)}
	if hasMemoryBlock(messages) {
		return out, nil
	}
	query := lastUser(messages)
	if query == "" {
		return out, nil
	}
	pkg, err := m.BuildContext(ctx, textutil.Truncate(query, 2000), memori.ContextOptions{
		TokenBudget:    opts.Budget,
		FactLimit:      opts.FactLimit,
		IncludeRaw:     opts.IncludeRaw,
		IncludeProfile: true,
	})
	if err != nil {
		return Result{}, err
	}
	out.Context = pkg
	text := strings.TrimSpace(pkg.Text)
	if text == "" {
		return out, nil
	}
	block := text + "\n" + Marker
	insertAt := 0
	for insertAt < len(out.Messages) && strings.EqualFold(out.Messages[insertAt].Role, "system") {
		insertAt++
	}
	next := make([]memori.ChatMessage, 0, len(out.Messages)+1)
	next = append(next, out.Messages[:insertAt]...)
	next = append(next, memori.ChatMessage{Role: "system", Content: block})
	next = append(next, out.Messages[insertAt:]...)
	out.Messages = next
	out.Injected = true
	return out, nil
}

func cloneMessages(in []memori.ChatMessage) []memori.ChatMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]memori.ChatMessage, len(in))
	copy(out, in)
	return out
}

func hasMemoryBlock(messages []memori.ChatMessage) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, Marker) ||
			strings.Contains(msg.Content, "<memories>") ||
			strings.Contains(msg.Content, "<memory_context>") ||
			strings.Contains(msg.Content, "<memory_profile>") {
			return true
		}
	}
	return false
}

func lastUser(messages []memori.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}
