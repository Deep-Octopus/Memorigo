package memori

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/storage"
)

var (
	ErrNoAttribution = errors.New("memori: attribution is required; call Attribution(entityID, processID) first")
	ErrEmptyContent  = errors.New("memori: content is empty")
	ErrClosed        = errors.New("memori: instance is closed")
)

// Memori is the SDK entry point. It is safe for concurrent use after
// construction; attribution and session switching are individually atomic but
// apply to the instance as a whole (use one instance per concurrent user).
type Memori struct {
	Config *Config

	Storage      *storage.Manager
	Augmentation *AugmentationManager
	Embedder     embed.Embedder

	OpenAI *OpenAIProvider

	extractor    Extractor
	ids          *idCache
	logger       *slog.Logger
	openAIClient *OpenAICompatClient
	aiMu         sync.Mutex

	initErr error

	attrMu  sync.RWMutex
	attrErr error

	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool

	lastModel atomic.Value // string
}

// Option configures a Memori instance during construction.
type Option func(*Memori)

// New builds a Memori instance. Storage migrations run automatically when a
// connection was supplied with WithStorageConn.
func New(opts ...Option) *Memori {
	m := &Memori{
		Config: newConfig(),
		ids:    newIDCache(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}

	if m.Storage == nil {
		m.Storage = storage.NewManager()
	}
	if m.logger == nil {
		m.logger = newLogger(m.Config.LogLevel)
	}
	if m.initErr == nil && m.Storage.Driver() != nil {
		if err := m.Storage.Build(); err != nil {
			m.initErr = err
			m.logger.Error("storage migration failed", "error", err)
		}
	}
	if m.Embedder == nil {
		cfg := m.Config.Embedding
		m.Embedder = embed.NewEmbedder(embed.Config{
			Provider:  cfg.Provider,
			APIKey:    cfg.APIKey,
			BaseURL:   cfg.BaseURL,
			Model:     cfg.Model,
			Dimension: cfg.Dimension,
			CacheSize: cfg.CacheSize,
		})
	}
	if m.extractor == nil {
		m.extractor = NewExtractor(m)
	}
	if m.Augmentation == nil {
		m.Augmentation = NewAugmentationManager(m)
	}
	m.OpenAI = &OpenAIProvider{m: m}
	return m
}

// WithStorageConn attaches a *sql.DB (SQLite/PostgreSQL) or *mongo.Database.
func WithStorageConn(conn any) Option {
	return func(m *Memori) {
		m.Storage = storage.NewManager()
		if err := m.Storage.Start(conn); err != nil {
			m.initErr = err
			return
		}
		m.Config.mu.Lock()
		m.Config.Storage.Dialect = m.Storage.Dialect()
		m.Config.mu.Unlock()
	}
}

// WithEmbedder overrides the embedding provider (useful for tests and custom
// providers).
func WithEmbedder(e embed.Embedder) Option {
	return func(m *Memori) { m.Embedder = e }
}

// WithExtractor overrides fact extraction.
func WithExtractor(e Extractor) Option {
	return func(m *Memori) { m.extractor = e }
}

// WithLogger installs a structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(m *Memori) {
		if l != nil {
			m.logger = l
		}
	}
}

// WithConfig mutates configuration before derived components are built. Use it
// for options that are not exposed as dedicated options.
func WithConfig(fn func(*Config)) Option {
	return func(m *Memori) {
		if fn != nil {
			fn(m.Config)
		}
	}
}

func newLogger(level string) *slog.Logger {
	if level == "" || level == "off" || level == "none" {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "info":
		lv = slog.LevelInfo
	case "warn", "warning":
		lv = slog.LevelWarn
	default:
		lv = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

// Err returns any deferred initialization or attribution error. Storage
// connection/migration failures and invalid Attribution calls are reported
// here instead of panicking.
func (m *Memori) Err() error {
	if m.initErr != nil {
		return m.initErr
	}
	m.attrMu.RLock()
	defer m.attrMu.RUnlock()
	return m.attrErr
}

// Attribution binds subsequent writes and recalls to an external user/agent
// identity. IDs are limited to 100 characters. Invalid IDs are reported by
// Err() and by subsequent operations; use TryAttribution to handle the error
// immediately.
func (m *Memori) Attribution(entityID, processID string) *Memori {
	if err := m.TryAttribution(entityID, processID); err != nil {
		m.attrMu.Lock()
		m.attrErr = err
		m.attrMu.Unlock()
	}
	return m
}

// TryAttribution is the error-returning variant of Attribution.
func (m *Memori) TryAttribution(entityID, processID string) error {
	if utf8.RuneCountInString(entityID) > 100 {
		return errors.New("memori: entity_id cannot be longer than 100 characters")
	}
	if utf8.RuneCountInString(processID) > 100 {
		return errors.New("memori: process_id cannot be longer than 100 characters")
	}
	m.Config.mu.Lock()
	m.Config.EntityID = entityID
	m.Config.ProcessID = processID
	m.Config.mu.Unlock()

	m.attrMu.Lock()
	m.attrErr = nil
	m.attrMu.Unlock()
	return nil
}

// NewSession starts a fresh conversation within the current attribution.
func (m *Memori) NewSession() *Memori {
	m.Config.mu.Lock()
	m.Config.SessionID = uuid.New()
	m.Config.mu.Unlock()
	return m
}

// SetSession resumes an existing session UUID.
func (m *Memori) SetSession(id uuid.UUID) *Memori {
	m.Config.mu.Lock()
	m.Config.SessionID = id
	m.Config.mu.Unlock()
	return m
}

// SessionID returns the active session UUID.
func (m *Memori) SessionID() uuid.UUID {
	m.Config.mu.RLock()
	defer m.Config.mu.RUnlock()
	return m.Config.SessionID
}

// SetAutoInject toggles automatic memory injection for the OpenAI-compatible
// client wrapper.
func (m *Memori) SetAutoInject(enabled bool) {
	m.Config.mu.Lock()
	m.Config.AutoInject = enabled
	m.Config.mu.Unlock()
}

// SetRecallLimit changes the default number of recalled facts.
func (m *Memori) SetRecallLimit(limit int) {
	if limit <= 0 {
		return
	}
	m.Config.mu.Lock()
	m.Config.RecallLimit = limit
	m.Config.mu.Unlock()
}

// SetMemoryPrompt overrides the system-prompt template used by auto-injection.
// The template must contain {{facts}}.
func (m *Memori) SetMemoryPrompt(template string) {
	if !strings.Contains(template, "{{facts}}") {
		return
	}
	m.Config.mu.Lock()
	m.Config.MemoryPrompt = template
	m.Config.mu.Unlock()
}

// Recall returns the most relevant active facts for a query.
func (m *Memori) Recall(query string, limit int) ([]Fact, error) {
	snap := m.Config.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	return m.RecallContext(ctx, query, limit)
}

// RecallContext is Recall with caller-controlled cancellation.
func (m *Memori) RecallContext(ctx context.Context, query string, limit int) ([]Fact, error) {
	if m.closed.Load() {
		return nil, ErrClosed
	}
	if err := m.Err(); err != nil {
		return nil, err
	}
	if m.Storage == nil || m.Storage.Driver() == nil {
		return nil, nil
	}
	repos, ok := m.Storage.Driver().(storage.Repos)
	if !ok {
		return nil, errors.New("memori: storage driver does not implement Repos")
	}

	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = snap.RecallLimit
	}
	if limit <= 0 {
		limit = 5
	}

	entityID, err := repos.Entity().GetByExternalID(ctx, snap.EntityID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var queryEmbedding []float32
	if strings.TrimSpace(query) != "" {
		var embedErr error
		queryEmbedding, embedErr = m.Embedder.EmbedText(ctx, query)
		if embedErr != nil {
			// Lexical ranking still works when the embedding provider is down.
			m.logger.Warn("query embedding failed; falling back to lexical recall", "error", embedErr)
			queryEmbedding = nil
		}
	}

	candidates, err := repos.EntityFact().Candidates(ctx, entityID, snap.Recall.ScanLimit)
	if err != nil {
		return nil, err
	}

	ranked := rankFacts(query, queryEmbedding, candidates, snap.Recall, time.Now())
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]Fact, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, Fact{
			ID:             r.fact.ID,
			Content:        r.fact.Content,
			Slot:           r.fact.Slot,
			Score:          r.score,
			Similarity:     r.similarity,
			LexicalScore:   r.lexical,
			NumTimes:       r.fact.NumTimes,
			DateLastTime:   r.fact.DateLastTime,
			ConversationID: r.fact.SourceConversationID,
		})
	}
	return out, nil
}

// MemoryContext recalls relevant facts and renders them with the configured
// memory prompt template. Useful when you manage prompts yourself.
func (m *Memori) MemoryContext(ctx context.Context, query string, limit int) (MemoryContext, error) {
	facts, err := m.RecallContext(ctx, query, limit)
	if err != nil {
		return MemoryContext{}, err
	}
	mc := MemoryContext{Facts: facts}
	if len(facts) > 0 {
		mc.Text = renderMemoryBlock(m.Config.snapshot().MemoryPrompt, facts)
	}
	return mc, nil
}

// ContextText is the plain-text form of MemoryContext.
func (m *Memori) ContextText(ctx context.Context, query string, limit int) (string, error) {
	mc, err := m.MemoryContext(ctx, query, limit)
	if err != nil {
		return "", err
	}
	return mc.Text, nil
}

// Remember stores an explicit long-term fact synchronously. It applies the
// same slot reconciliation as automatic extraction, so remembering a new home
// city supersedes the old one.
func (m *Memori) Remember(content string) (int64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, ErrEmptyContent
	}
	if m.closed.Load() {
		return 0, ErrClosed
	}
	if err := m.Err(); err != nil {
		return 0, err
	}
	repos, err := m.repos()
	if err != nil {
		return 0, err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return 0, ErrNoAttribution
	}

	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()

	entityID, err := m.ids.entity(ctx, repos, snap.EntityID)
	if err != nil {
		return 0, err
	}

	candidates := extractCandidatesFromText(content)
	if len(candidates) == 0 {
		candidates = []CandidateFact{{Content: content}}
	}
	ids, err := m.Augmentation.storeFacts(ctx, entityID, nil, candidates, true)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, errors.New("memori: fact was not stored")
	}
	return ids[0], nil
}

// Forget soft-deletes a fact so it no longer appears in recall.
func (m *Memori) Forget(factID int64) error {
	if m.closed.Load() {
		return ErrClosed
	}
	repos, err := m.repos()
	if err != nil {
		return err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return ErrNoAttribution
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	entityID, err := repos.Entity().GetByExternalID(ctx, snap.EntityID)
	if err != nil {
		return err
	}
	unlock := m.Augmentation.lockEntity(entityID)
	defer unlock()
	return repos.EntityFact().Forget(ctx, entityID, factID)
}

// Facts lists active facts, most recent first.
func (m *Memori) Facts(limit int) ([]Fact, error) {
	repos, err := m.repos()
	if err != nil {
		return nil, err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return nil, ErrNoAttribution
	}
	if limit <= 0 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	entityID, err := repos.Entity().GetByExternalID(ctx, snap.EntityID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := repos.EntityFact().List(ctx, entityID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Fact, 0, len(rows))
	for _, r := range rows {
		out = append(out, Fact{
			ID:             r.ID,
			Content:        r.Content,
			Slot:           r.Slot,
			NumTimes:       r.NumTimes,
			DateLastTime:   r.DateLastTime,
			ConversationID: r.SourceConversationID,
		})
	}
	return out, nil
}

// Stats reports memory counts and component metadata.
func (m *Memori) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{
		EntityID:          m.Config.snapshot().EntityID,
		EmbeddingProvider: m.Embedder.Provider(),
		EmbeddingModel:    m.Embedder.Model(),
		EmbeddingDim:      m.Embedder.Dimension(),
		StorageDialect:    m.Storage.Dialect(),
		PendingJobs:       m.Augmentation.Pending(),
	}
	repos, err := m.repos()
	if err != nil {
		return stats, err
	}
	if stats.EntityID == "" {
		return stats, nil
	}
	entityID, err := repos.Entity().GetByExternalID(ctx, stats.EntityID)
	if errors.Is(err, storage.ErrNotFound) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	total, active, err := repos.EntityFact().Count(ctx, entityID)
	if err != nil {
		return stats, err
	}
	stats.TotalFacts = total
	stats.ActiveFacts = active
	return stats, nil
}

// Reindex re-embeds active facts whose embedding model or dimension no longer
// matches the configured embedder. Run it after switching embedding providers.
func (m *Memori) Reindex(ctx context.Context) (int, error) {
	repos, err := m.repos()
	if err != nil {
		return 0, err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return 0, ErrNoAttribution
	}
	entityID, err := repos.Entity().GetByExternalID(ctx, snap.EntityID)
	if err != nil {
		return 0, err
	}

	tag := embeddingTag(m.Embedder)
	dim := m.Embedder.Dimension()
	reindexed := 0
	for {
		if err := ctx.Err(); err != nil {
			return reindexed, err
		}
		batch, err := repos.EntityFact().ListForReembed(ctx, entityID, tag, dim, 64)
		if err != nil {
			return reindexed, err
		}
		if len(batch) == 0 {
			break
		}
		texts := make([]string, len(batch))
		for i, f := range batch {
			texts[i] = f.Content
		}
		vectors, err := m.Embedder.EmbedTexts(ctx, texts)
		if err != nil {
			return reindexed, err
		}
		if len(vectors) != len(batch) {
			return reindexed, embed.ErrEmbeddingDimensionMismatch
		}
		batchDim := len(vectors[0])
		if batchDim <= 0 {
			return reindexed, embed.ErrEmbeddingDimensionMismatch
		}
		for _, vec := range vectors {
			if len(vec) != batchDim {
				return reindexed, embed.ErrEmbeddingDimensionMismatch
			}
		}
		for i, f := range batch {
			if err := repos.EntityFact().UpdateEmbedding(ctx, f.ID, storage.EncodeEmbedding(vectors[i]), tag, batchDim); err != nil {
				return reindexed, err
			}
			reindexed++
		}
		// A custom endpoint may return a different dimension than the
		// embedder originally reported. Continue with the actual dimension,
		// otherwise ListForReembed keeps selecting the just-updated rows.
		dim = batchDim
		if len(batch) < 64 {
			break
		}
	}
	return reindexed, nil
}

// Flush waits until all queued augmentation jobs have been processed.
func (m *Memori) Flush(ctx context.Context) error {
	if m.Augmentation == nil {
		return nil
	}
	return m.Augmentation.Flush(ctx)
}

// Close stops accepting augmentation jobs, drains the queue and waits for
// in-flight work. It does not close the user-owned database connection.
func (m *Memori) Close() error {
	m.closeOnce.Do(func() {
		m.closed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if m.Augmentation != nil {
			m.closeErr = m.Augmentation.Close(ctx)
		}
	})
	return m.closeErr
}

func (m *Memori) repos() (storage.Repos, error) {
	if err := m.Err(); err != nil {
		return nil, err
	}
	if m.Storage == nil || m.Storage.Driver() == nil {
		return nil, errors.New("memori: no storage configured; use WithStorageConn")
	}
	repos, ok := m.Storage.Driver().(storage.Repos)
	if !ok {
		return nil, errors.New("memori: storage driver does not implement Repos")
	}
	return repos, nil
}

func (m *Memori) getOpenAIClient() *OpenAICompatClient {
	m.aiMu.Lock()
	defer m.aiMu.Unlock()
	return m.openAIClient
}

func (m *Memori) getOrCreateOpenAIClient() *OpenAICompatClient {
	m.aiMu.Lock()
	defer m.aiMu.Unlock()
	if m.openAIClient == nil {
		m.openAIClient = NewOpenAIClient()
	}
	return m.openAIClient
}

func (m *Memori) setOpenAIClient(client *OpenAICompatClient) {
	m.aiMu.Lock()
	m.openAIClient = client
	m.aiMu.Unlock()
}

func (m *Memori) recordModel(model string) {
	if model != "" {
		m.lastModel.Store(model)
	}
}

func (m *Memori) lastUsedModel() string {
	if v := m.lastModel.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func embeddingTag(e embed.Embedder) string {
	model := strings.TrimSpace(e.Model())
	if model == "" {
		return e.Provider()
	}
	return e.Provider() + ":" + model
}

// CosineSimilarity is re-exported for callers that work with raw vectors.
func CosineSimilarity(a, b []float32) float64 {
	return embed.CosineSimilarity(a, b)
}
