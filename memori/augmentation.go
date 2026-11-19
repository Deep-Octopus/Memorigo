package memori

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Deep-Octopus/Memorigo/pii"
	"github.com/Deep-Octopus/Memorigo/storage"
	"github.com/Deep-Octopus/Memorigo/textutil"
)

// AugmentationInput is one queued memory-extraction job.
type AugmentationInput struct {
	ConversationID int64
	EntityID       string
	ProcessID      string
	Messages       []Message
	Response       *Message
}

type augmentationJob struct {
	input AugmentationInput
}

// AugmentationManager runs fact extraction/embedding off the hot path. Jobs
// are bounded and dropped only when the queue is full; Flush and Close give
// callers deterministic drain semantics.
type AugmentationManager struct {
	m       *Memori
	queue   chan augmentationJob
	workers int

	startOnce sync.Once
	sendMu    sync.RWMutex
	closed    bool
	pending   atomic.Int64
	// storeLocks serialize fact reconciliation per entity. Parallel exclusive
	// slot writes would otherwise interleave Upsert/SupersedeSlot and can leave
	// zero or multiple active values.
	storeLocks [64]sync.Mutex
}

func NewAugmentationManager(m *Memori) *AugmentationManager {
	workers := envInt("MEMORI_AUGMENTATION_WORKERS", 0)
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
		if workers < 2 {
			workers = 2
		}
	}
	queueSize := envInt("MEMORI_AUGMENTATION_QUEUE", 1024)
	if queueSize <= 0 {
		queueSize = 1024
	}
	return &AugmentationManager{
		m:       m,
		queue:   make(chan augmentationJob, queueSize),
		workers: workers,
	}
}

func (m *AugmentationManager) Start() {
	m.startOnce.Do(func() {
		for i := 0; i < m.workers; i++ {
			go m.worker(i)
		}
	})
}

// Enqueue adds a job without blocking. A full queue drops the oldest-pressure
// job; the drop is logged and counted by Pending only while queued.
func (m *AugmentationManager) Enqueue(in AugmentationInput) {
	if in.EntityID == "" {
		return
	}
	m.Start()

	m.sendMu.RLock()
	defer m.sendMu.RUnlock()
	if m.closed {
		return
	}

	m.pending.Add(1)
	select {
	case m.queue <- augmentationJob{input: in}:
	default:
		m.pending.Add(-1)
		m.m.logger.Warn("augmentation queue full; dropping memory job",
			"entity_id", in.EntityID, "conversation_id", in.ConversationID)
	}
}

func (m *AugmentationManager) worker(id int) {
	for job := range m.queue {
		func() {
			defer func() {
				if r := recover(); r != nil {
					m.m.logger.Error("augmentation worker panic", "worker", id, "panic", r)
				}
				m.pending.Add(-1)
			}()
			if err := m.processWithRetry(job.input); err != nil {
				m.m.logger.Error("augmentation failed", "worker", id, "error", err)
			}
		}()
	}
}

func (m *AugmentationManager) processWithRetry(in AugmentationInput) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = m.processInput(in)
		if err == nil {
			return nil
		}
		if attempt == 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return err
}

func (m *AugmentationManager) processInput(in AugmentationInput) error {
	if m.m.Storage == nil || m.m.Storage.Driver() == nil {
		return nil
	}
	repos, ok := m.m.Storage.Driver().(storage.Repos)
	if !ok {
		return errors.New("storage driver does not implement Repos")
	}

	snap := m.m.Config.snapshot()
	timeout := snap.Extraction.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	entityID, err := repos.Entity().GetByExternalID(ctx, in.EntityID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// The writer always creates the entity first; a missing row means
			// the job outlived a manual cleanup. Skip quietly.
			return nil
		}
		return err
	}

	extraction, err := m.m.extractor.Extract(ctx, ExtractionInput{Messages: in.Messages, Response: in.Response})
	if err != nil {
		m.m.logger.Warn("extractor failed; falling back to heuristic", "error", err)
		extraction, err = (&HeuristicExtractor{maxFacts: snap.Extraction.MaxFacts}).Extract(ctx, ExtractionInput{
			Messages: in.Messages,
			Response: in.Response,
		})
		if err != nil {
			return err
		}
	}

	var sourceConversationID *int64
	if in.ConversationID > 0 {
		id := in.ConversationID
		sourceConversationID = &id
	}
	ids, err := m.storeFacts(ctx, entityID, sourceConversationID, extraction.Facts, false)
	if err != nil {
		return err
	}

	if snap.Extraction.SummaryEnabled && extraction.Summary != "" && in.ConversationID > 0 {
		existing, getErr := repos.Conversation().GetSummary(ctx, in.ConversationID)
		if getErr != nil && !errors.Is(getErr, storage.ErrNotFound) {
			m.m.logger.Warn("failed to read conversation summary", "error", getErr)
		}
		summary := mergeSummary(existing, extraction.Summary)
		if snap.Privacy.Redact {
			summary = pii.Redact(summary)
		}
		if err := repos.Conversation().UpdateSummary(ctx, in.ConversationID, summary); err != nil {
			m.m.logger.Warn("failed to update conversation summary", "error", err)
		}
	}

	m.m.logger.Debug("augmentation complete",
		"entity_id", in.EntityID,
		"facts", len(extraction.Facts),
		"stored", len(ids))
	return nil
}

func (m *AugmentationManager) lockEntity(entityID int64) func() {
	lock := &m.storeLocks[uint64(entityID)%uint64(len(m.storeLocks))]
	lock.Lock()
	return lock.Unlock
}

// storeFacts embeds, deduplicates, reconciles and persists candidate facts. It
// is shared by the async pipeline and the synchronous Remember API.
func (m *AugmentationManager) storeFacts(ctx context.Context, entityID int64, sourceConversationID *int64, candidates []CandidateFact, reactivate bool) ([]int64, error) {
	if m.m.Storage == nil || m.m.Storage.Driver() == nil {
		return nil, errors.New("memori: no storage configured")
	}
	repos, ok := m.m.Storage.Driver().(storage.Repos)
	if !ok {
		return nil, errors.New("storage driver does not implement Repos")
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	unlock := m.lockEntity(entityID)
	defer unlock()

	clean := make([]CandidateFact, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		content := cleanSentence(c.Content)
		if content == "" {
			continue
		}
		if m.m.Config.snapshot().Privacy.Redact {
			content = pii.Redact(content)
		}
		key := textutil.Key(content)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		c.Content = content
		if c.Exclusive || isExclusiveSlot(c.Slot) {
			c.Exclusive = true
		}
		clean = append(clean, c)
	}
	if len(clean) == 0 {
		return nil, nil
	}

	texts := make([]string, len(clean))
	for i, c := range clean {
		texts[i] = c.Content
	}
	vectors, err := m.m.Embedder.EmbedTexts(ctx, texts)
	if err != nil {
		return nil, err
	}

	snap := m.m.Config.snapshot()
	tag := embeddingTag(m.m.Embedder)

	// Load active candidates once for semantic near-duplicate merging. This is
	// only needed when at least one new fact has no exclusive slot.
	var mergeCandidates []storage.FactResult
	needMerge := false
	for _, c := range clean {
		if !c.Exclusive && snap.Extraction.MergeThreshold > 0 {
			needMerge = true
			break
		}
	}
	if needMerge {
		mergeCandidates, err = repos.EntityFact().Candidates(ctx, entityID, snap.Recall.ScanLimit)
		if err != nil {
			return nil, err
		}
	}

	var sourceMessageID *int64
	if sourceConversationID != nil {
		if id, ok, msgErr := repos.Message().LatestUserMessageID(ctx, *sourceConversationID); msgErr != nil {
			return nil, msgErr
		} else if ok {
			sourceMessageID = &id
		}
	}

	factRepo := repos.EntityFact()
	ids := make([]int64, 0, len(clean))
	for i, c := range clean {
		vec := vectors[i]
		uniq := contentHash(c.Content)

		// Conversation replay must not undo an explicit forget or a newer
		// slot value. Only explicit Remember calls may reactivate a fact.
		if !reactivate {
			inactive, err := factRepo.HasInactiveUniq(ctx, entityID, uniq)
			if err != nil {
				return nil, err
			}
			if inactive {
				continue
			}
		}

		if needMerge && !c.Exclusive {
			bestID, bestScore := int64(0), 0.0
			for _, existing := range mergeCandidates {
				if existing.Slot != c.Slot {
					continue
				}
				if len(existing.Embedding) != len(vec) {
					continue
				}
				sim := CosineSimilarity(vec, existing.Embedding)
				if sim > bestScore {
					bestScore = sim
					bestID = existing.ID
				}
			}
			if bestID != 0 && bestScore >= snap.Extraction.MergeThreshold {
				if err := factRepo.Touch(ctx, bestID); err != nil {
					return nil, err
				}
				ids = append(ids, bestID)
				continue
			}
		}

		id, err := factRepo.Upsert(ctx, entityID, storage.FactInput{
			Content:              c.Content,
			Embedding:            storage.EncodeEmbedding(vec),
			Uniq:                 uniq,
			Slot:                 c.Slot,
			EmbeddingModel:       tag,
			EmbeddingDim:         len(vec),
			SourceConversationID: sourceConversationID,
			SourceMessageID:      sourceMessageID,
			Confidence:           c.Confidence,
			AssertedBy:           c.AssertedBy,
			PreserveInactive:     !reactivate,
		})
		if err != nil {
			return nil, err
		}
		if c.Exclusive && c.Slot != "" {
			if err := factRepo.SupersedeSlot(ctx, entityID, c.Slot, id); err != nil {
				return nil, err
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Flush waits for all currently queued and in-flight jobs to finish.
func (m *AugmentationManager) Flush(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if m.pending.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Close stops accepting jobs, drains the queue and waits for completion.
func (m *AugmentationManager) Close(ctx context.Context) error {
	m.sendMu.Lock()
	if !m.closed {
		m.closed = true
		close(m.queue)
	}
	m.sendMu.Unlock()
	return m.Flush(ctx)
}

// Pending returns the number of queued or in-flight jobs.
func (m *AugmentationManager) Pending() int64 { return m.pending.Load() }

// DropOldest is intentionally not implemented: dropping is handled at enqueue
// time to keep the main request path non-blocking.

func contentHash(content string) string {
	key := textutil.Key(content)
	if key == "" {
		key = textutil.Normalize(content)
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// mergeSummary keeps a conversation summary cumulative when clients send only
// the newest turn instead of the full history.
func mergeSummary(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" {
		return textutil.Truncate(incoming, 1000)
	}
	if incoming == "" {
		return existing
	}
	if strings.Contains(existing, incoming) {
		return existing
	}
	if strings.Contains(incoming, existing) {
		return textutil.Truncate(incoming, 1000)
	}
	return textutil.Truncate(existing+"；"+incoming, 1000)
}
