package memori

import (
	"context"
	"errors"
	"fmt"
	"time"

	"memorigo/storage"
)

const (
	maxRetries       = 3
	retryBackoffBase = 100 * time.Millisecond
)

type ConversationPayload struct {
	Client struct {
		Provider string
		Title    string
	}
	Messages []Message
	Response *Message
}

type Message struct {
	Role    string
	Type    string
	Content string
}

type Writer struct {
	m *Memori
}

func NewWriter(m *Memori) *Writer {
	return &Writer{m: m}
}

func (w *Writer) Execute(ctx context.Context, payload ConversationPayload) error {
	if w.m.Storage == nil || w.m.Storage.Driver() == nil {
		return nil
	}

	driver := w.m.Storage.Driver()
	repos, ok := driver.(storage.Repos)
	if !ok {
		return fmt.Errorf("driver does not implement Repos interface")
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := w.executeTransaction(ctx, repos, payload)
		if err == nil {
			return nil
		}

		// Check if retriable error (CockroachDB "restart transaction")
		if isRetriableError(err) && attempt < maxRetries-1 {
			time.Sleep(retryBackoffBase * time.Duration(1<<attempt))
			continue
		}
		return err
	}
	return errors.New("max retries exceeded")
}

func (w *Writer) executeTransaction(ctx context.Context, repos storage.Repos, payload ConversationPayload) error {
	cfg := w.m.Config
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// Ensure entity_id cached
	var entityID *int64
	if cfg.EntityID != "" {
		id, err := w.ensureCachedID("entity_id", func() (int64, error) {
			return repos.Entity().Create(cfg.EntityID)
		})
		if err != nil {
			return err
		}
		entityID = &id
	}

	// Ensure process_id cached
	var processID *int64
	if cfg.ProcessID != "" {
		id, err := w.ensureCachedID("process_id", func() (int64, error) {
			return repos.Process().Create(cfg.ProcessID)
		})
		if err != nil {
			return err
		}
		processID = &id
	}

	// Ensure session_id cached
	sessionID, err := w.ensureCachedID("session_id", func() (int64, error) {
		return repos.Session().Create(entityID, processID, cfg.SessionID)
	})
	if err != nil {
		return err
	}

	// Ensure conversation_id cached
	conversationID, err := w.ensureCachedID("conversation_id", func() (int64, error) {
		return repos.Conversation().Create(sessionID, int(cfg.SessionTTL.Minutes()))
	})
	if err != nil {
		return err
	}

	// Write messages (skip system role)
	msgRepo := repos.Message()
	for _, msg := range payload.Messages {
		if msg.Role != "system" {
			if err := msgRepo.Create(conversationID, msg.Role, msg.Type, msg.Content); err != nil {
				return err
			}
		}
	}

	// Write response
	if payload.Response != nil {
		if err := msgRepo.Create(conversationID, payload.Response.Role, payload.Response.Type, payload.Response.Content); err != nil {
			return err
		}
	}

	// Fire-and-forget offline augmentation
	w.m.Augmentation.Enqueue(AugmentationInput{
		ConversationID: conversationID,
		EntityID:       cfg.EntityID,
		ProcessID:      cfg.ProcessID,
		Messages:       payload.Messages,
	})

	return nil
}

func (w *Writer) ensureCachedID(cacheKey string, createFunc func() (int64, error)) (int64, error) {
	cache := w.m.Config.Cache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	var cachedID *int64
	switch cacheKey {
	case "entity_id":
		cachedID = cache.EntityID
	case "process_id":
		cachedID = cache.ProcessID
	case "session_id":
		cachedID = cache.SessionID
	case "conversation_id":
		cachedID = cache.ConversationID
	}

	if cachedID != nil {
		return *cachedID, nil
	}

	id, err := createFunc()
	if err != nil {
		return 0, err
	}

	// Update cache
	switch cacheKey {
	case "entity_id":
		cache.EntityID = &id
	case "process_id":
		cache.ProcessID = &id
	case "session_id":
		cache.SessionID = &id
	case "conversation_id":
		cache.ConversationID = &id
	}

	return id, nil
}

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "restart transaction") || contains(errStr, "serialization failure")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOfSubstring(s, substr) >= 0)
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}


