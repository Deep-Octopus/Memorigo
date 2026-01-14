package memori

import (
	"errors"

	"github.com/google/uuid"

	"memorigo/embed"
	"memorigo/storage"
)

type Memori struct {
	Config *Config

	Storage      *storage.Manager
	Augmentation *AugmentationManager
	Embedder     embed.Embedder

	OpenAI *OpenAIProvider

	openAIClient *OpenAICompatClient
}

type Option func(*Memori)

func New(opts ...Option) *Memori {
	m := &Memori{
		Config: newConfig(),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Defaults
	if m.Storage == nil {
		m.Storage = storage.NewManager()
	}
	if m.Augmentation == nil {
		m.Augmentation = NewAugmentationManager(m)
	}
	if m.Embedder == nil {
		m.Embedder = embed.NewEmbedder(embed.Config{
			Provider: m.Config.Embedding.Provider,
			APIKey:   m.Config.Embedding.APIKey,
			BaseURL:  m.Config.Embedding.BaseURL,
			Model:    m.Config.Embedding.Model,
		})
	}

	m.OpenAI = &OpenAIProvider{m: m}
	return m
}

func WithStorageConn(conn any) Option {
	return func(m *Memori) {
		m.Storage = storage.NewManager()
		_ = m.Storage.Start(conn)
		m.Config.Storage.Dialect = m.Storage.Dialect()
	}
}

func (m *Memori) Attribution(entityID, processID string) *Memori {
	if len(entityID) > 100 {
		panic("entity_id cannot be greater than 100 characters")
	}
	if len(processID) > 100 {
		panic("process_id cannot be greater than 100 characters")
	}
	m.Config.mu.Lock()
	defer m.Config.mu.Unlock()
	m.Config.EntityID = entityID
	m.Config.ProcessID = processID
	return m
}

func (m *Memori) NewSession() *Memori {
	m.Config.mu.Lock()
	m.Config.SessionID = uuid.New()
	m.Config.mu.Unlock()
	m.Config.ResetCache()
	return m
}

func (m *Memori) SetSession(id uuid.UUID) *Memori {
	m.Config.mu.Lock()
	defer m.Config.mu.Unlock()
	m.Config.SessionID = id
	return m
}

func (m *Memori) Recall(query string, limit int) ([]Fact, error) {
	if m.Storage == nil || m.Storage.Driver() == nil {
		return nil, nil
	}
	if m.Config.EntityID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = m.Config.RecallLimit
	}

	r := NewRecall(m)
	return r.SearchFacts(query, limit)
}

var ErrNotImplemented = errors.New("not implemented")

// CosineSimilarity 计算两个向量的余弦相似度
func CosineSimilarity(a, b []float32) float64 {
	return embed.CosineSimilarity(a, b)
}
