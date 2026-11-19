package storage

import (
	"context"
	"database/sql"
	"fmt"
)

type sqlSubjectRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlSubjectRepo) Facts(ctx context.Context, entityID int64) ([]SubjectFact, error) {
	query := fmt.Sprintf(`
SELECT id, content, slot, status, num_times, date_last_time,
       valid_from, valid_to, source_message_id, source_conversation_id,
       confidence, asserted_by
FROM memori_entity_fact
WHERE entity_id = %s
ORDER BY id`, sqlPlaceholder(r.dialect, 1))
	rows, err := r.q.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubjectFact
	for rows.Next() {
		var fact SubjectFact
		var slot, status, asserted sql.NullString
		var last, validFrom, validTo any
		var messageID, conversationID sql.NullInt64
		var confidence sql.NullFloat64
		if err := rows.Scan(
			&fact.ID, &fact.Content, &slot, &status, &fact.NumTimes, &last,
			&validFrom, &validTo, &messageID, &conversationID,
			&confidence, &asserted,
		); err != nil {
			return nil, err
		}
		fact.Slot = slot.String
		fact.Status = status.String
		if fact.Status == "" {
			fact.Status = "active"
		}
		if t, ok := decodeAnyTime(last); ok {
			fact.DateLastTime = t
		}
		if t, ok := decodeAnyTime(validFrom); ok {
			fact.ValidFrom = t
		}
		if t, ok := decodeAnyTime(validTo); ok {
			fact.ValidTo = &t
		}
		if messageID.Valid {
			id := messageID.Int64
			fact.SourceMessageID = &id
		}
		if conversationID.Valid {
			id := conversationID.Int64
			fact.SourceConversationID = &id
		}
		fact.Confidence = confidence.Float64
		if fact.Confidence == 0 {
			fact.Confidence = 1
		}
		fact.AssertedBy = asserted.String
		if fact.AssertedBy == "" {
			fact.AssertedBy = "user"
		}
		out = append(out, fact)
	}
	return out, rows.Err()
}

func (r *sqlSubjectRepo) Messages(ctx context.Context, entityID int64) ([]StoredMessage, error) {
	query := fmt.Sprintf(`
SELECT m.id, m.conversation_id, m.role, m.content, m.date_created
FROM memori_conversation_message m
JOIN memori_conversation c ON c.id = m.conversation_id
JOIN memori_session s ON s.id = c.session_id
WHERE s.entity_id = %s
ORDER BY m.id`, sqlPlaceholder(r.dialect, 1))
	rows, err := r.q.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, err
	}
	return scanStoredMessages(rows, 16)
}

func (r *sqlSubjectRepo) Delete(ctx context.Context, entityID int64) (DeletionReport, error) {
	var report DeletionReport
	var err error
	ph := sqlPlaceholder(r.dialect, 1)
	report.Messages, err = execCount(ctx, r.q, fmt.Sprintf(`
DELETE FROM memori_conversation_message
WHERE conversation_id IN (
  SELECT c.id FROM memori_conversation c
  JOIN memori_session s ON s.id = c.session_id
  WHERE s.entity_id = %s
)`, ph), entityID)
	if err != nil {
		return report, err
	}
	report.Conversations, err = execCount(ctx, r.q, fmt.Sprintf(`
DELETE FROM memori_conversation
WHERE session_id IN (SELECT id FROM memori_session WHERE entity_id = %s)`, ph), entityID)
	if err != nil {
		return report, err
	}
	report.Sessions, err = execCount(ctx, r.q, "DELETE FROM memori_session WHERE entity_id = "+ph, entityID)
	if err != nil {
		return report, err
	}
	report.Facts, err = execCount(ctx, r.q, "DELETE FROM memori_entity_fact WHERE entity_id = "+ph, entityID)
	if err != nil {
		return report, err
	}
	report.GraphEdges, err = execCount(ctx, r.q, "DELETE FROM memori_knowledge_graph WHERE entity_id = "+ph, entityID)
	if err != nil {
		return report, err
	}
	if err := deleteUnreferencedVocab(ctx, r.q, r.dialect); err != nil {
		return report, err
	}
	report.Entities, err = execCount(ctx, r.q, "DELETE FROM memori_entity WHERE id = "+ph, entityID)
	return report, err
}

// deleteUnreferencedVocab removes subject, predicate and object rows that no
// knowledge-graph edge references. Those tables are global. Under Postgres
// RLS the edge subquery would only see the current tenant, so a tenant with
// no remaining edges would delete every other tenant's words. The bypass
// reads every edge, then row security is turned back on for the entity delete.
func deleteUnreferencedVocab(ctx context.Context, q sqlQuerier, dialect string) error {
	stmts := []string{
		`DELETE FROM memori_subject WHERE id NOT IN (SELECT subject_id FROM memori_knowledge_graph)`,
		`DELETE FROM memori_predicate WHERE id NOT IN (SELECT predicate_id FROM memori_knowledge_graph)`,
		`DELETE FROM memori_object WHERE id NOT IN (SELECT object_id FROM memori_knowledge_graph)`,
	}
	bypass, _ := q.(interface {
		ExecBypass(context.Context, string, ...any) (sql.Result, error)
	})
	useBypass := dialect == "postgres" && rlsMode() == "on"
	if useBypass && bypass == nil {
		return fmt.Errorf("storage: MEMORI_RLS=on cannot clean graph vocabulary without seeing every tenant's edges")
	}
	for _, stmt := range stmts {
		var err error
		if useBypass {
			_, err = bypass.ExecBypass(ctx, stmt)
		} else {
			_, err = execCount(ctx, q, stmt)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func execCount(ctx context.Context, q sqlQuerier, query string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}
