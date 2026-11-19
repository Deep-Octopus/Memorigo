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
	Provider  string // "openai", "siliconflow", "ollama", "hash"
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
	LexicalLimit     int
	MinScore         float64
	MinSimilarity    float64
	WeightSimilarity float64
	WeightLexical    float64
	WeightRecency    float64
	WeightFrequency  float64
	RecencyHalfLife  time.Duration
	// Fusion is "weighted" (default) or "rrf". RRF is weighted reciprocal
	// rank fusion and can be rolled back by setting Fusion to "weighted".
	Fusion string
	// RRFK is the reciprocal-rank constant. Zero uses the usual value of 60.
	RRFK int
	// IncludeInferred recalls facts asserted by the assistant or by inference.
	// User-asserted facts are always eligible. Default is false.
	IncludeInferred bool
	// RerankTopN is how many already-ranked facts are sent to an optional
	// reranker. Zero disables the pass even when a reranker is installed.
	RerankTopN int
}

// RerankConfig selects an optional HTTP reranker. Provider "off" (the default)
// installs nothing, so recall stays on the hybrid score.
type RerankConfig struct {
	Provider string // "off" or "http"
	URL      string
	APIKey   string
	Model    string
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
	OrgID       string
	AppID       string
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
	Rerank     RerankConfig
	Profile    ProfileConfig
	Privacy    PrivacyConfig
	Storage    StorageConfig
	LLM        LLMConfig
}

// configSnapshot is a lock-free copy of the fields used by request paths.
type configSnapshot struct {
	EntityID      string
	ProcessID     string
	OrgID         string
	AppID         string
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
	Privacy       PrivacyConfig
}

func newConfig() *Config {
	c := &Config{
		OrgID:         envString("MEMORI_ORG_ID", ""),
		AppID:         envString("MEMORI_APP_ID", ""),
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
			LexicalLimit:     envInt("MEMORI_RECALL_LEXICAL_LIMIT", 150),
			MinScore:         envFloat("MEMORI_RECALL_MIN_SCORE", 0.05),
			MinSimilarity:    envFloat("MEMORI_RECALL_MIN_SIMILARITY", 0.10),
			WeightSimilarity: envFloat("MEMORI_RECALL_WEIGHT_SIMILARITY", 0.60),
			WeightLexical:    envFloat("MEMORI_RECALL_WEIGHT_LEXICAL", 0.20),
			WeightRecency:    envFloat("MEMORI_RECALL_WEIGHT_RECENCY", 0.10),
			WeightFrequency:  envFloat("MEMORI_RECALL_WEIGHT_FREQUENCY", 0.10),
			RecencyHalfLife:  envDuration("MEMORI_RECALL_RECENCY_HALF_LIFE", 7*24*time.Hour),
			Fusion:           normalizeFusion(envString("MEMORI_RECALL_FUSION", "weighted")),
			RRFK:             envInt("MEMORI_RECALL_RRF_K", 60),
			IncludeInferred:  envBool("MEMORI_RECALL_INCLUDE_INFERRED", false),
			RerankTopN:       rerankTopN(),
		},
		Rerank: RerankConfig{
			Provider: normalizeRerankProvider(envString("MEMORI_RERANK", "off")),
			URL:      envString("MEMORI_RERANK_URL", ""),
			APIKey:   envString("MEMORI_RERANK_API_KEY", ""),
			Model:    envString("MEMORI_RERANK_MODEL", ""),
		},
		Profile: ProfileConfig{
			Budget:         envInt("MEMORI_PROFILE_BUDGET", 200),
			Limit:          envInt("MEMORI_PROFILE_LIMIT", 40),
			IncludeDynamic: envBool("MEMORI_PROFILE_DYNAMIC", false),
		},
		Privacy: PrivacyConfig{
			Redact: envBool("MEMORI_PII_REDACT", false),
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
		OrgID:         c.OrgID,
		AppID:         c.AppID,
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
		Privacy:       c.Privacy,
	}
}

func normalizeRerankProvider(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "http":
		return "http"
	default:
		return "off"
	}
}

// rerankTopN defaults to 20 only when the HTTP reranker is selected and the
// caller did not set MEMORI_RERANK_TOP_N. An explicit 0 keeps the pass off.
func rerankTopN() int {
	if _, set := os.LookupEnv("MEMORI_RERANK_TOP_N"); set {
		return envInt("MEMORI_RERANK_TOP_N", 0)
	}
	if normalizeRerankProvider(envString("MEMORI_RERANK", "off")) == "http" {
		return 20
	}
	return 0
}

func normalizeFusion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "rrf":
		return "rrf"
	default:
		return "weighted"
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
