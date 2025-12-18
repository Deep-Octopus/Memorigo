package memori

import (
	"context"
	"strings"
)

// MemoriOpenAIClient wraps OpenAICompatClient and automatically persists:
// - request messages
// - final assistant response (non-stream or streamed accumulation)
// Then it triggers offline augmentation (writer already enqueues).
type MemoriOpenAIClient struct {
	m   *Memori
	raw *OpenAICompatClient
}

func (m *Memori) OpenAIClient() *MemoriOpenAIClient {
	if m.openAIClient == nil {
		// Default to OpenAI env-based client (may still work for providers that don't require auth)
		m.openAIClient = NewOpenAIClient()
		// keep config info reasonable
		m.Config.mu.Lock()
		if m.Config.LLM.Provider == "" {
			m.Config.LLM.Provider = "openai_compatible"
			m.Config.LLM.Version = "v1"
		}
		m.Config.mu.Unlock()
	}
	return &MemoriOpenAIClient{m: m, raw: m.openAIClient}
}

func (c *MemoriOpenAIClient) ChatCompletionsCreate(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	resp, err := c.raw.ChatCompletionsCreate(ctx, req)
	if err != nil {
		return resp, err
	}

	// Best-effort persistence: do not fail the LLM call if local storage fails.
	_ = c.persist(req, resp, "")
	return resp, nil
}

func (c *MemoriOpenAIClient) ChatCompletionsStream(ctx context.Context, req ChatCompletionsRequest) (<-chan StreamEvent, <-chan error) {
	inEvents, inErrs := c.raw.ChatCompletionsStream(ctx, req)

	outEvents := make(chan StreamEvent, 128)
	outErrs := make(chan error, 1)

	go func() {
		defer close(outEvents)
		defer close(outErrs)

		var b strings.Builder
		var lastResp ChatCompletionsResponse

		for {
			select {
			case ev, ok := <-inEvents:
				if !ok {
					// Stream ended without explicit [DONE]
					if b.Len() > 0 || len(lastResp.Choices) > 0 {
						_ = c.persist(req, lastResp, b.String())
					}
					return
				}

				outEvents <- ev

				if ev.Chunk != nil {
					lastResp = *ev.Chunk
					if len(ev.Chunk.Choices) > 0 {
						b.WriteString(ev.Chunk.Choices[0].Delta.Content)
					}
				}

				if ev.Done {
					_ = c.persist(req, lastResp, b.String())
					return
				}

			case err, ok := <-inErrs:
				if ok && err != nil {
					outErrs <- err
				}
				// Persist partial content best-effort
				if b.Len() > 0 || len(lastResp.Choices) > 0 {
					_ = c.persist(req, lastResp, b.String())
				}
				return
			}
		}

	}()

	return outEvents, outErrs
}

func (c *MemoriOpenAIClient) persist(req ChatCompletionsRequest, resp ChatCompletionsResponse, streamedText string) error {
	// Convert request messages
	msgs := make([]Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, Message{
			Role:    m.Role,
			Type:    "",
			Content: m.Content,
		})
	}

	// Decide assistant output
	assistant := streamedText
	if assistant == "" && len(resp.Choices) > 0 {
		assistant = resp.Choices[0].Message.Content
	}

	payload := ConversationPayload{
		Messages: msgs,
		Response: &Message{Role: "assistant", Type: "text", Content: assistant},
	}
	payload.Client.Provider = "openai_compatible"
	payload.Client.Title = req.Model

	// Note: Writer.Execute triggers offline augmentation (enqueue) internally.
	return NewWriter(c.m).Execute(context.Background(), payload)
}


