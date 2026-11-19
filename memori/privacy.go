package memori

import (
	"context"
	"errors"
	"time"

	"github.com/Deep-Octopus/Memorigo/pii"
	"github.com/Deep-Octopus/Memorigo/storage"
)

const exportVersion = 1

// PrivacyConfig controls opt-in write-time redaction. Export can redact a copy
// without this switch. Neither path is a general anonymizer.
type PrivacyConfig struct {
	// Redact masks emails, phone numbers and Luhn-valid card numbers before
	// they are stored. Default is false.
	Redact bool
}

// ExportOptions controls a subject export.
type ExportOptions struct {
	// Redact masks emails, phones and card numbers in the exported copy.
	// Stored rows are unchanged.
	Redact bool
}

// ExportFact is one fact in a portable export, including inactive rows.
type ExportFact struct {
	ID             int64      `json:"id"`
	Content        string     `json:"content"`
	Slot           string     `json:"slot,omitempty"`
	Status         string     `json:"status"`
	NumTimes       int64      `json:"num_times"`
	DateLastTime   time.Time  `json:"date_last_time"`
	ValidFrom      time.Time  `json:"valid_from,omitempty"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	MessageID      *int64     `json:"message_id,omitempty"`
	ConversationID *int64     `json:"conversation_id,omitempty"`
	Confidence     float64    `json:"confidence"`
	AssertedBy     string     `json:"asserted_by"`
}

// ExportMessage is one stored conversation message.
type ExportMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	DateCreated    time.Time `json:"date_created"`
}

// SubjectExport is version 1 of the portable subject file. Embeddings are
// omitted. Process rows are not included.
type SubjectExport struct {
	Version    int             `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	EntityID   string          `json:"entity_id"`
	OrgID      string          `json:"org_id,omitempty"`
	AppID      string          `json:"app_id,omitempty"`
	Facts      []ExportFact    `json:"facts"`
	Messages   []ExportMessage `json:"messages"`
}

// DeletionReport counts rows removed by DeleteSubject. A second call is a
// no-op and reports zeros. This is a hard delete of the subject's rows, not
// cryptographic shredding.
type DeletionReport struct {
	Facts         int64 `json:"facts"`
	Messages      int64 `json:"messages"`
	Conversations int64 `json:"conversations"`
	Sessions      int64 `json:"sessions"`
	GraphEdges    int64 `json:"graph_edges"`
	Entities      int64 `json:"entities"`
}

// Export returns every fact and message for the current entity. A subject with
// no rows returns an empty version-1 document.
func (m *Memori) Export(opts ExportOptions) (SubjectExport, error) {
	out := SubjectExport{Version: exportVersion, ExportedAt: time.Now().UTC()}
	if m.closed.Load() {
		return out, ErrClosed
	}
	repos, err := m.repos()
	if err != nil {
		return out, err
	}
	snap := m.Config.snapshot()
	out.EntityID = snap.EntityID
	out.OrgID = snap.OrgID
	out.AppID = snap.AppID
	if snap.EntityID == "" {
		return out, ErrNoAttribution
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	entityID, err := repos.Entity().GetByExternalID(ctx, entityKey(snap))
	if errors.Is(err, storage.ErrNotFound) {
		out.Facts = []ExportFact{}
		out.Messages = []ExportMessage{}
		return out, nil
	}
	if err != nil {
		return out, err
	}
	facts, err := repos.Subject().Facts(ctx, entityID)
	if err != nil {
		return out, err
	}
	messages, err := repos.Subject().Messages(ctx, entityID)
	if err != nil {
		return out, err
	}
	out.Facts = make([]ExportFact, 0, len(facts))
	for _, fact := range facts {
		content := fact.Content
		if opts.Redact {
			content = pii.Redact(content)
		}
		out.Facts = append(out.Facts, ExportFact{
			ID:             fact.ID,
			Content:        content,
			Slot:           fact.Slot,
			Status:         fact.Status,
			NumTimes:       fact.NumTimes,
			DateLastTime:   fact.DateLastTime,
			ValidFrom:      fact.ValidFrom,
			ValidTo:        fact.ValidTo,
			MessageID:      fact.SourceMessageID,
			ConversationID: fact.SourceConversationID,
			Confidence:     fact.Confidence,
			AssertedBy:     fact.AssertedBy,
		})
	}
	out.Messages = make([]ExportMessage, 0, len(messages))
	for _, msg := range messages {
		content := msg.Content
		if opts.Redact {
			content = pii.Redact(content)
		}
		out.Messages = append(out.Messages, ExportMessage{
			ID:             msg.ID,
			ConversationID: msg.ConversationID,
			Role:           msg.Role,
			Content:        content,
			DateCreated:    msg.DateCreated,
		})
	}
	return out, nil
}

// DeleteSubject permanently removes the current entity and the facts, messages,
// sessions and knowledge-graph edges that belong to it. Another entity in the
// same database is left intact. The agent process row is kept.
func (m *Memori) DeleteSubject() (DeletionReport, error) {
	if m.closed.Load() {
		return DeletionReport{}, ErrClosed
	}
	if err := m.Err(); err != nil {
		return DeletionReport{}, err
	}
	if m.Storage == nil || m.Storage.Driver() == nil {
		return DeletionReport{}, errors.New("memori: no storage configured; use WithStorageConn")
	}
	repos, err := m.repos()
	if err != nil {
		return DeletionReport{}, err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return DeletionReport{}, ErrNoAttribution
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	key := entityKey(snap)
	entityID, err := repos.Entity().GetByExternalID(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return DeletionReport{}, nil
	}
	if err != nil {
		return DeletionReport{}, err
	}
	if m.Augmentation != nil {
		unlock := m.Augmentation.lockEntity(entityID)
		defer unlock()
	}
	var stored storage.DeletionReport
	run := func(tx storage.Repos) error {
		var delErr error
		stored, delErr = tx.Subject().Delete(ctx, entityID)
		return delErr
	}
	driver := m.Storage.Driver()
	if tx, ok := driver.(storage.Transactional); ok {
		err = tx.Transaction(ctx, run)
	} else {
		err = run(repos)
	}
	if err != nil {
		return DeletionReport{}, err
	}
	if m.ids != nil {
		m.ids.forgetEntity(key)
	}
	return DeletionReport{
		Facts:         stored.Facts,
		Messages:      stored.Messages,
		Conversations: stored.Conversations,
		Sessions:      stored.Sessions,
		GraphEdges:    stored.GraphEdges,
		Entities:      stored.Entities,
	}, nil
}

func redactConversation(payload ConversationPayload) ConversationPayload {
	if len(payload.Messages) > 0 {
		msgs := make([]Message, len(payload.Messages))
		for i, msg := range payload.Messages {
			msg.Content = pii.Redact(msg.Content)
			msgs[i] = msg
		}
		payload.Messages = msgs
	}
	if payload.Response != nil {
		resp := *payload.Response
		resp.Content = pii.Redact(resp.Content)
		payload.Response = &resp
	}
	return payload
}
