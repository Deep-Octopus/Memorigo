package memori

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EmbeddingConfig selects and configures the embedding provider.
type EmbeddingConfig struct {
	Provider  string // "openai", "siliconflow", "hash"
	APIKey    string
	BaseURL   string
	Model     string
	Dimension int
	CacheSize int
}

// ExtractionConfig controls how long-term facts are derived from conversations.
type ExtractionConfig struct {
	Provider       string // "heuristic", "llm", "off"
	Model          string // model used when Provider == "llm"
	MaxFacts       int
	MergeThreshold float64 // semantic near-duplicate threshold for slot-less facts
	SummaryEnabled bool
	Timeout        time.Duration
}

// RecallConfig controls candidate scanning and hybrid ranking.
type RecallConfig struct {
	ScanLimit        int
	MinScore         float64
	MinSimilarity    float64
	WeightSimilarity float64
	WeightLexical    float64
	WeightRecency    float64
	WeightFrequency  float64
	RecencyHalfLife  time.Duration
}

// ProfileConfig controls the always-on profile injected independently of the
// current query.
type ProfileConfig struct {
	Budget         int
	Limit          int
	IncludeDynamic bool
}

// StorageConfig records the detected storage dialect.
type StorageConfig struct {
	Dialect string
}

// LLMConfig records the registered LLM provider metadata.
type LLMConfig struct {
	Provider string
	Version  string
}

// Config holds per-Memori runtime configuration. Exported fields are intended
// to be set at construction time (WithConfig); use the Memori setters for
// changes while calls are in flight.
type Config struct {
	mu sync.RWMutex

	EntityID    string
	ProcessID   string
	SessionID   uuid.UUID
	RecallLimit int

	AutoInject    bool
	InjectLimit   int
	MemoryPrompt  string
	ContextBudget int

	OpTimeout time.Duration
	LogLevel  string

	Embedding  EmbeddingConfig
	Extraction ExtractionConfig
	Recall     RecallConfig
	Profile    ProfileConfig
	Storage    StorageConfig
	LLM        LLMConfig
}

// configSnapshot is a lock-free copy of the fields used by request paths.
type configSnapshot struct {
	EntityID      string
	ProcessID     string
	SessionID     uuid.UUID
	RecallLimit   int
	AutoInject    bool
	InjectLimit   int
	MemoryPrompt  string
	ContextBudget int
	OpTimeout     time.Duration
	Embedding     EmbeddingConfig
	Extraction    ExtractionConfig
	Recall        RecallConfig
	Profile       ProfileConfig
}

func newConfig() *Config {
	c := &Config{
		SessionID:     uuid.New(),
		RecallLimit:   envInt("MEMORI_RECALL_LIMIT", 5),
		AutoInject:    envBool("MEMORI_AUTO_INJECT", true),
		InjectLimit:   envInt("MEMORI_INJECT_LIMIT", 5),
		MemoryPrompt:  envString("MEMORI_MEMORY_PROMPT", DefaultMemoryPrompt),
		ContextBudget: envInt("MEMORI_CONTEXT_BUDGET", defaultContextBudget),
		OpTimeout:     envDuration("MEMORI_OP_TIMEOUT", 20*time.Second),
		LogLevel:      strings.ToLower(envString("MEMORI_LOG_LEVEL", "error")),
		Embedding: EmbeddingConfig{
			Provider:  envString("MEMORI_EMBEDDING_PROVIDER", "hash"),
			APIKey:    envString("MEMORI_EMBEDDING_API_KEY", ""),
			BaseURL:   envString("MEMORI_EMBEDDING_BASE_URL", ""),
			Model:     envString("MEMORI_EMBEDDING_MODEL", ""),
			Dimension: envInt("MEMORI_EMBEDDING_DIMENSION", 0),
			CacheSize: envInt("MEMORI_EMBEDDING_CACHE_SIZE", 1024),
		},
		Extraction: ExtractionConfig{
			Provider:       strings.ToLower(envString("MEMORI_EXTRACTION_PROVIDER", "heuristic")),
			Model:          envString("MEMORI_EXTRACTION_MODEL", ""),
			MaxFacts:       envInt("MEMORI_EXTRACTION_MAX_FACTS", 20),
			MergeThreshold: envFloat("MEMORI_EXTRACTION_MERGE_THRESHOLD", 0.97),
			SummaryEnabled: envBool("MEMORI_EXTRACTION_SUMMARY", true),
			Timeout:        envDuration("MEMORI_EXTRACTION_TIMEOUT", 30*time.Second),
		},
		Recall: RecallConfig{
			ScanLimit:        envInt("MEMORI_RECALL_SCAN_LIMIT", 2000),
			MinScore:         envFloat("MEMORI_RECALL_MIN_SCORE", 0.05),
			MinSimilarity:    envFloat("MEMORI_RECALL_MIN_SIMILARITY", 0.10),
			WeightSimilarity: envFloat("MEMORI_RECALL_WEIGHT_SIMILARITY", 0.60),
			WeightLexical:    envFloat("MEMORI_RECALL_WEIGHT_LEXICAL", 0.20),
			WeightRecency:    envFloat("MEMORI_RECALL_WEIGHT_RECENCY", 0.10),
			WeightFrequency:  envFloat("MEMORI_RECALL_WEIGHT_FREQUENCY", 0.10),
			RecencyHalfLife:  envDuration("MEMORI_RECALL_RECENCY_HALF_LIFE", 7*24*time.Hour),
		},
		Profile: ProfileConfig{
			Budget:         envInt("MEMORI_PROFILE_BUDGET", 200),
			Limit:          envInt("MEMORI_PROFILE_LIMIT", 40),
			IncludeDynamic: envBool("MEMORI_PROFILE_DYNAMIC", false),
		},
	}
	return c
}

func (c *Config) snapshot() configSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return configSnapshot{
		EntityID:      c.EntityID,
		ProcessID:     c.ProcessID,
		SessionID:     c.SessionID,
		RecallLimit:   c.RecallLimit,
		AutoInject:    c.AutoInject,
		InjectLimit:   c.InjectLimit,
		MemoryPrompt:  c.MemoryPrompt,
		ContextBudget: c.ContextBudget,
		OpTimeout:     c.OpTimeout,
		Embedding:     c.Embedding,
		Extraction:    c.Extraction,
		Recall:        c.Recall,
		Profile:       c.Profile,
	}
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
