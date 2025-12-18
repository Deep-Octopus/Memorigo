package memori

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"

	"memorigo/storage"
)

type AugmentationInput struct {
	ConversationID any
	EntityID       string
	ProcessID      string
	Messages       []Message
	SystemPrompt   string
}

type AugmentationManager struct {
	m         *Memori
	startOnce sync.Once
	queue     chan AugmentationInput
	workers   int
}

func NewAugmentationManager(m *Memori) *AugmentationManager {
	return &AugmentationManager{
		m:       m,
		queue:   make(chan AugmentationInput, 1000),
		workers: 8,
	}
}

func (m *AugmentationManager) Start() {
	m.startOnce.Do(func() {
		for i := 0; i < m.workers; i++ {
			go m.worker()
		}
	})
}

func (m *AugmentationManager) Enqueue(input AugmentationInput) {
	if input.EntityID == "" {
		return
	}
	m.Start()
	select {
	case m.queue <- input:
	default:
		// drop when queue is full (non-blocking, keep main path low-latency)
	}
}

func (m *AugmentationManager) worker() {
	for in := range m.queue {
		m.processInput(in)
	}
}

// processInput implements a simple offline augmentation pipeline:
// - treat non-empty user/assistant messages as candidate facts
// - compute deterministic embeddings
// - upsert into memori_entity_fact
// - create a simple conversation summary from recent messages
func (m *AugmentationManager) processInput(in AugmentationInput) {
	drv := m.m.Storage.Driver()
	if drv == nil {
		return
	}
	repos, ok := drv.(storage.Repos)
	if !ok {
		return
	}

	// Resolve internal entity id
	entityID, err := repos.Entity().GetByExternalID(in.EntityID)
	if err != nil {
		return
	}

	// Extract candidate facts
	var facts []string
	for _, msg := range in.Messages {
		if msg.Role == "system" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		// Very simple heuristic: take full message as fact, but trim overly long ones
		if len(content) > 500 {
			content = content[:500]
		}
		facts = append(facts, content)
	}

	// Upsert entity facts
	factRepo := repos.EntityFact()
	for _, f := range facts {
		emb := embedText(f)
		embBytes := encodeEmbedding(emb)
		uniq := hashString(f)
		_ = factRepo.Upsert(entityID, f, embBytes, uniq)
	}

	// Update conversation summary if we have an id
	if in.ConversationID != nil {
		if convID, ok := in.ConversationID.(int64); ok {
			summary := buildSummary(in.Messages)
			_ = repos.Conversation().UpdateSummary(convID, summary)
		}
	}
}

// encodeEmbedding serializes []float32 into []byte (little-endian).
func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:])
}

func buildSummary(msgs []Message) string {
	const maxLen = 512
	var parts []string
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		parts = append(parts, text)
		if len(strings.Join(parts, "\n")) > maxLen {
			break
		}
	}
	s := strings.Join(parts, "\n")
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// placeholder to keep API shape stable; will be used for cancellation in future
func (m *AugmentationManager) Shutdown(ctx context.Context) error {
	_ = ctx
	return nil
}

