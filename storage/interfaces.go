package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by repositories when a requested row does not exist.
var ErrNotFound = errors.New("storage: not found")

// Repos is the storage-neutral repository bundle handed to the writer and
// augmentation pipeline. SQL drivers provide a transactional implementation.
type Repos interface {
	Entity() EntityRepo
	Process() ProcessRepo
	Session() SessionRepo
	Conversation() ConversationRepo
	Message() MessageRepo
	EntityFact() EntityFactRepo
}

// Transactional is implemented by drivers that can run a repository bundle in
// a real database transaction. Mongo standalone deployments are not
// transactional; the Mongo driver runs the callback without one.
type Transactional interface {
	Transaction(ctx context.Context, fn func(Repos) error) error
}

type EntityRepo interface {
	Create(ctx context.Context, externalID string) (int64, error)
	GetByExternalID(ctx context.Context, externalID string) (int64, error)
}

type ProcessRepo interface {
	Create(ctx context.Context, externalID string) (int64, error)
	GetByExternalID(ctx context.Context, externalID string) (int64, error)
}

type SessionRepo interface {
	Create(ctx context.Context, entityID, processID *int64, sessionUUID uuid.UUID) (int64, error)
	GetByUUID(ctx context.Context, sessionUUID uuid.UUID) (int64, error)
}

type ConversationRepo interface {
	// Create returns the session's existing conversation or creates one. The
	// schema enforces one conversation per session; start a new session (and
	// therefore a new conversation) with Memori.NewSession().
	Create(ctx context.Context, sessionID int64) (int64, error)
	GetBySessionID(ctx context.Context, sessionID int64) (int64, error)
	UpdateSummary(ctx context.Context, conversationID int64, summary string) error
	GetSummary(ctx context.Context, conversationID int64) (string, error)
}

// StoredMessage is the minimal representation used to avoid re-persisting a
// full chat history that clients resend on every turn. IDs and conversation IDs
// are stable citations for context assembly.
type StoredMessage struct {
	ID             int64
	ConversationID int64
	Role           string
	Content        string
	DateCreated    time.Time
}

type MessageRepo interface {
	Create(ctx context.Context, conversationID int64, role, msgType, content string) error
	Recent(ctx context.Context, conversationID int64, limit int) ([]StoredMessage, error)
	// RecentForEntity returns recent messages across every conversation owned by
	// the entity. It powers the raw-evidence channel of context assembly.
	RecentForEntity(ctx context.Context, entityID int64, limit int) ([]StoredMessage, error)
	// LatestUserMessageID returns the newest user message in a conversation.
	// The boolean is false when the conversation has no user message.
	LatestUserMessageID(ctx context.Context, conversationID int64) (int64, bool, error)
}

// FactInput is a candidate long-term memory fact.
type FactInput struct {
	Content              string
	Embedding            []byte
	Uniq                 string
	Slot                 string
	EmbeddingModel       string
	EmbeddingDim         int
	SourceConversationID *int64
	SourceMessageID      *int64
	// Confidence is in (0, 1]. Zero stores the default of 1.
	Confidence float64
	// AssertedBy is user, assistant, or inferred. Empty stores user.
	AssertedBy string
	// PreserveInactive keeps an existing superseded/deleted row inactive.
	// Automatic extraction sets it so a multi-process race cannot resurrect a
	// fact the user forgot; explicit Remember leaves it false.
	PreserveInactive bool
}

// FactResult is a stored fact with provenance. Score is filled by the recall
// ranking layer, not by storage.
type FactResult struct {
	ID                   int64
	Content              string
	Slot                 string
	Embedding            []float32
	Score                float64
	NumTimes             int64
	DateLastTime         time.Time
	EmbeddingDim         int
	EmbeddingModel       string
	SourceConversationID *int64
	ValidFrom            time.Time
	ValidTo              *time.Time
	SourceMessageID      *int64
	Confidence           float64
	AssertedBy           string
}

// FactForReembed is a lightweight row used by the re-embedding maintenance
// path when the embedding provider or dimension changes.
type FactForReembed struct {
	ID      int64
	Content string
}

type EntityFactRepo interface {
	// Upsert inserts a fact or bumps the existing fact with the same
	// (entity_id, uniq), returning its id.
	Upsert(ctx context.Context, entityID int64, in FactInput) (int64, error)
	// Touch bumps the frequency counter and last-seen timestamp.
	Touch(ctx context.Context, factID int64) error
	// HasInactiveUniq reports whether the entity already has a superseded or
	// deleted fact with this content hash. Automatic extraction uses it to
	// avoid resurrecting facts a user updated or explicitly forgot.
	HasInactiveUniq(ctx context.Context, entityID int64, uniq string) (bool, error)
	// SupersedeSlot marks every other active fact in the same exclusive slot
	// as superseded. Used for profile attributes such as name or home city.
	SupersedeSlot(ctx context.Context, entityID int64, slot string, exceptID int64) error
	// Candidates returns active facts for an entity, newest first, capped at
	// scanLimit. Recall performs hybrid scoring on this candidate set.
	Candidates(ctx context.Context, entityID int64, scanLimit int) ([]FactResult, error)
	// SearchLexical returns active facts whose indexed tokens overlap the
	// query. It is a candidate generator for facts that have fallen outside
	// the recency scan window. An empty result is not an error.
	SearchLexical(ctx context.Context, entityID int64, query string, limit int) ([]FactResult, error)
	// List returns active facts ordered by recency and frequency.
	List(ctx context.Context, entityID int64, limit int) ([]FactResult, error)
	// Forget soft-deletes a fact (status=deleted) so it is excluded from recall.
	Forget(ctx context.Context, entityID int64, factID int64) error
	// Count returns total and active fact counts for an entity.
	Count(ctx context.Context, entityID int64) (total int64, active int64, err error)
	ListForReembed(ctx context.Context, entityID int64, model string, dim int, limit int) ([]FactForReembed, error)
	UpdateEmbedding(ctx context.Context, factID int64, embedding []byte, model string, dim int) error
}
