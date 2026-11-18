package memori

import (
	"context"
	"strings"
	"time"
)

// MemoriOpenAIClient wraps OpenAICompatClient with automatic memory injection
// and conversation persistence.
type MemoriOpenAIClient struct {
	m   *Memori
	raw *OpenAICompatClient
}

// OpenAIClient returns the memory-aware client. If no client was registered,
// it falls back to an environment-configured OpenAI client.
func (m *Memori) OpenAIClient() *MemoriOpenAIClient {
	raw := m.getOrCreateOpenAIClient()
	m.Config.mu.Lock()
	if m.Config.LLM.Provider == "" {
		m.Config.LLM.Provider = "openai_compatible"
		m.Config.LLM.Version = "v1"
	}
	m.Config.mu.Unlock()
	return &MemoriOpenAIClient{m: m, raw: raw}
}

func (c *MemoriOpenAIClient) ChatCompletionsCreate(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	c.m.recordModel(req.Model)
	original := req.Messages
	req.Messages = c.m.prepareMessages(ctx, original)

	resp, err := c.raw.ChatCompletionsCreate(ctx, req)
	if err != nil {
		return resp, err
	}
	c.persist(original, req.Model, resp, "")
	return resp, nil
}

func (c *MemoriOpenAIClient) ChatCompletionsStream(ctx context.Context, req ChatCompletionsRequest) (<-chan StreamEvent, <-chan error) {
	c.m.recordModel(req.Model)
	original := req.Messages
	req.Messages = c.m.prepareMessages(ctx, original)

	inEvents, inErrs := c.raw.ChatCompletionsStream(ctx, req)

	outEvents := make(chan StreamEvent, 128)
	outErrs := make(chan error, 1)

	go func() {
		defer close(outEvents)
		defer close(outErrs)

		var b strings.Builder
		var lastResp ChatCompletionsResponse
		received := false
		persisted := false
		persist := func() {
			if persisted || !received {
				return
			}
			persisted = true
			c.persist(original, req.Model, lastResp, b.String())
		}

		// errCh is set to nil once the upstream error channel closes without an
		// error. The event channel may still hold buffered chunks at that point,
		// so a closed error channel must NOT terminate the forwarder early.
		errCh := inErrs
		for {
			select {
			case ev, ok := <-inEvents:
				if !ok {
					persist()
					if errCh != nil {
						for err := range errCh {
							if err != nil {
								select {
								case outErrs <- err:
								default:
								}
							}
						}
					}
					return
				}
				if ev.Chunk != nil {
					received = true
					lastResp = *ev.Chunk
					if len(ev.Chunk.Choices) > 0 {
						b.WriteString(ev.Chunk.Choices[0].Delta.Content)
					}
				}
				select {
				case outEvents <- ev:
				case <-ctx.Done():
					persist()
					return
				}
				if ev.Done {
					persist()
					return
				}

			case err, ok := <-errCh:
				if !ok {
					errCh = nil
					continue
				}
				if err != nil {
					select {
					case outErrs <- err:
					default:
					}
					persist()
					return
				}
			}
		}
	}()

	return outEvents, outErrs
}

func (c *MemoriOpenAIClient) persist(messages []ChatMessage, model string, resp ChatCompletionsResponse, streamedText string) {
	if c.m.Storage == nil || c.m.Storage.Driver() == nil {
		return
	}
	msgs := make([]Message, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, Message{Role: m.Role, Type: "text", Content: m.Content})
	}

	assistant := streamedText
	if assistant == "" && len(resp.Choices) > 0 {
		assistant = resp.Choices[0].Message.Content
	}
	payload := ConversationPayload{Messages: msgs}
	if strings.TrimSpace(assistant) != "" {
		payload.Response = &Message{Role: "assistant", Type: "text", Content: assistant}
	}
	payload.Client.Provider = "openai_compatible"
	payload.Client.Title = model

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := NewWriter(c.m).Execute(ctx, payload); err != nil {
		c.m.logger.Warn("failed to persist conversation", "error", err)
	}
}
