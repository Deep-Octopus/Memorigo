package memori

import (
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Cache struct {
	mu             sync.RWMutex
	ConversationID *int64
	EntityID       *int64
	ProcessID      *int64
	SessionID      *int64
}

type LLMConfig struct {
	Provider string
	Version  string
}

type StorageConfig struct {
	Dialect string
}

type Config struct {
	mu sync.RWMutex

	APIKey      string
	Enterprise  bool
	EntityID    string
	ProcessID   string
	SessionID   uuid.UUID
	Cache       Cache
	LLM         LLMConfig
	Storage     StorageConfig
	Timeout     time.Duration
	SessionTTL  time.Duration
	RecallLimit int
}

func newConfig() *Config {
	c := &Config{
		APIKey:      os.Getenv("MEMORI_API_KEY"),
		Enterprise:  os.Getenv("MEMORI_ENTERPRISE") == "1",
		SessionID:   uuid.New(),
		Timeout:     10 * time.Second,
		SessionTTL:  30 * time.Minute,
		RecallLimit: 5,
	}
	return c
}

func (c *Config) ResetCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Cache = Cache{}
}


