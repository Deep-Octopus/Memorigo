package memori

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Deep-Octopus/Memorigo/storage"
)

const (
	maxRetries       = 3
	retryBackoffBase = 100 * time.Millisecond
	recentMessageCap = 500
)

// ConversationPayload is one completed LLM exchange to persist.
type ConversationPayload struct {
	Client struct {
		Provider string
		Title    string
	}
	Messages []Message
	Response *Message
}

// Message is a persisted conversation message.
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

// Execute persists a conversation exchange and schedules offline augmentation.
// It is idempotent with respect to chat histories that clients resend on every
// turn: only the suffix not already stored is appended.
func (w *Writer) Execute(ctx context.Context, payload ConversationPayload) error {
	if w.m.closed.Load() {
		return ErrClosed
	}
	if err := w.m.Err(); err != nil {
		return err
	}
	if w.m.Storage == nil || w.m.Storage.Driver() == nil {
		return nil
	}
	driver := w.m.Storage.Driver()
	repos, ok := driver.(storage.Repos)
	if !ok {
		return errors.New("memori: storage driver does not implement Repos")
	}
	snap := w.m.Config.snapshot()

	var conversationID int64
	var freshMessages []Message
	var wrote bool
	run := func(txRepos storage.Repos) error {
		id, fresh, changed, err := w.executeTransaction(ctx, txRepos, payload, snap)
		if err == nil {
			conversationID = id
			freshMessages = fresh
			wrote = changed
		}
		return err
	}

	var err error
	if tx, ok := driver.(storage.Transactional); ok {
		for attempt := 0; attempt < maxRetries; attempt++ {
			err = tx.Transaction(ctx, run)
			if err == nil {
				break
			}
			if isRetriableError(err) && attempt < maxRetries-1 {
				time.Sleep(retryBackoffBase * time.Duration(1<<attempt))
				continue
			}
			break
		}
	} else {
		err = run(repos)
	}
	if err != nil {
		return err
	}

	// Enqueue only after the transaction commits so the augmentation worker
	// can see the entity/session rows. A replay that wrote nothing must not
	// inflate frequency counts or pay for another extraction pass; only the
	// unseen suffix is sent to extraction, never the whole client history.
	if wrote && conversationID > 0 && snap.EntityID != "" {
		w.m.Augmentation.Enqueue(AugmentationInput{
			ConversationID: conversationID,
			EntityID:       entityKey(snap),
			ProcessID:      snap.ProcessID,
			Messages:       freshMessages,
			Response:       payload.Response,
		})
	}
	return nil
}

func (w *Writer) executeTransaction(ctx context.Context, repos storage.Repos, payload ConversationPayload, snap configSnapshot) (int64, []Message, bool, error) {
	var entityID *int64
	if snap.EntityID != "" {
		// Do not use the process-wide id cache inside a transaction: cached
		// ids created here become visible to other goroutines before commit
		// and would survive a rollback. The repository Create methods are
		// get-or-create, so the extra round-trip is bounded.
		id, err := repos.Entity().Create(ctx, entityKey(snap))
		if err != nil {
			return 0, nil, false, err
		}
		entityID = &id
	}

	var processID *int64
	if snap.ProcessID != "" {
		id, err := repos.Process().Create(ctx, snap.ProcessID)
		if err != nil {
			return 0, nil, false, err
		}
		processID = &id
	}

	sessionID, err := repos.Session().Create(ctx, entityID, processID, snap.SessionID)
	if err != nil {
		return 0, nil, false, err
	}

	conversationID, err := repos.Conversation().Create(ctx, sessionID)
	if err != nil {
		return 0, nil, false, err
	}

	stored, err := repos.Message().Recent(ctx, conversationID, recentMessageCap)
	if err != nil {
		return 0, nil, false, err
	}
	incoming := nonSystemMessages(payload.Messages)
	fresh := splitNewMessages(stored, incoming)

	wrote := false
	msgRepo := repos.Message()
	for _, msg := range fresh {
		msgType := msg.Type
		if msgType == "" {
			msgType = "text"
		}
		if err := msgRepo.Create(ctx, conversationID, msg.Role, msgType, msg.Content); err != nil {
			return 0, nil, false, fmt.Errorf("write message: %w", err)
		}
		wrote = true
	}

	if payload.Response != nil && strings.TrimSpace(payload.Response.Content) != "" {
		role := payload.Response.Role
		if role == "" {
			role = "assistant"
		}
		msgType := payload.Response.Type
		if msgType == "" {
			msgType = "text"
		}
		// Some callers include the assistant reply in Messages and also set
		// Response. Do not persist the same message twice.
		duplicate := len(incoming) > 0 &&
			incoming[len(incoming)-1].Role == role &&
			incoming[len(incoming)-1].Content == payload.Response.Content
		if !duplicate {
			if err := msgRepo.Create(ctx, conversationID, role, msgType, payload.Response.Content); err != nil {
				return 0, nil, false, fmt.Errorf("write response: %w", err)
			}
			wrote = true
		}
	}
	return conversationID, fresh, wrote, nil
}

func nonSystemMessages(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// splitNewMessages finds the largest overlap between the incoming history
// prefix and the stored history suffix, then returns only the unseen suffix.
// This handles both clients that resend full history and clients that send only
// the newest message.
func splitNewMessages(stored []storage.StoredMessage, incoming []Message) []Message {
	if len(stored) == 0 || len(incoming) == 0 {
		return incoming
	}
	maxOverlap := len(stored)
	if len(incoming) < maxOverlap {
		maxOverlap = len(incoming)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		storedTail := stored[len(stored)-overlap:]
		match := true
		for i := 0; i < overlap; i++ {
			if storedTail[i].Role != incoming[i].Role || storedTail[i].Content != incoming[i].Content {
				match = false
				break
			}
		}
		if match {
			return incoming[overlap:]
		}
	}
	return incoming
}

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "restart transaction") ||
		strings.Contains(msg, "serialization failure") ||
		strings.Contains(msg, "deadlock detected")
}
