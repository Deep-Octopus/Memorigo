package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

// sqlQuerier is implemented by *sql.DB and *sql.Tx so every repository can run
// either standalone or inside a transaction without duplication.
type sqlQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func sqlPlaceholder(dialect string, n int) string {
	if dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func mapSQLNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type sqlEntityRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlEntityRepo) Create(ctx context.Context, externalID string) (int64, error) {
	if id, err := r.GetByExternalID(ctx, externalID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	query := fmt.Sprintf(
		"INSERT INTO memori_entity (uuid, external_id, org_id, app_id, date_created) VALUES (%s, %s, %s, %s, %s) ON CONFLICT DO NOTHING RETURNING id",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
		sqlPlaceholder(r.dialect, 4), sqlPlaceholder(r.dialect, 5),
	)
	org, app, _ := ScopeParts(externalID)
	var id int64
	err := r.q.QueryRowContext(ctx, query, uuid.New().String(), externalID, org, app, time.Now()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		// Another writer created it first. ON CONFLICT keeps a PostgreSQL
		// transaction usable; a caught unique violation would abort it.
		return r.GetByExternalID(ctx, externalID)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *sqlEntityRepo) GetByExternalID(ctx context.Context, externalID string) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_entity WHERE external_id = " + sqlPlaceholder(r.dialect, 1)
	err := r.q.QueryRowContext(ctx, query, externalID).Scan(&id)
	if err != nil {
		return 0, mapSQLNotFound(err)
	}
	return id, nil
}

type sqlProcessRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlProcessRepo) Create(ctx context.Context, externalID string) (int64, error) {
	if id, err := r.GetByExternalID(ctx, externalID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	query := fmt.Sprintf(
		"INSERT INTO memori_process (uuid, external_id, date_created) VALUES (%s, %s, %s) ON CONFLICT DO NOTHING RETURNING id",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
	)
	var id int64
	err := r.q.QueryRowContext(ctx, query, uuid.New().String(), externalID, time.Now()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return r.GetByExternalID(ctx, externalID)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *sqlProcessRepo) GetByExternalID(ctx context.Context, externalID string) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_process WHERE external_id = " + sqlPlaceholder(r.dialect, 1)
	err := r.q.QueryRowContext(ctx, query, externalID).Scan(&id)
	if err != nil {
		return 0, mapSQLNotFound(err)
	}
	return id, nil
}

type sqlSessionRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlSessionRepo) Create(ctx context.Context, entityID, processID *int64, sessionUUID uuid.UUID) (int64, error) {
	var id int64
	query := fmt.Sprintf(
		"INSERT INTO memori_session (uuid, entity_id, process_id, date_created) VALUES (%s, %s, %s, %s) ON CONFLICT DO NOTHING RETURNING id",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
		sqlPlaceholder(r.dialect, 3), sqlPlaceholder(r.dialect, 4),
	)
	err := r.q.QueryRowContext(
		ctx, query,
		sessionUUID.String(), nullableInt64(entityID), nullableInt64(processID), time.Now(),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return r.GetByUUID(ctx, sessionUUID)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *sqlSessionRepo) GetByUUID(ctx context.Context, sessionUUID uuid.UUID) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_session WHERE uuid = " + sqlPlaceholder(r.dialect, 1)
	err := r.q.QueryRowContext(ctx, query, sessionUUID.String()).Scan(&id)
	if err != nil {
		return 0, mapSQLNotFound(err)
	}
	return id, nil
}

type sqlConversationRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlConversationRepo) Create(ctx context.Context, sessionID int64) (int64, error) {
	var existingID int64
	querySel := "SELECT id FROM memori_conversation WHERE session_id = " +
		sqlPlaceholder(r.dialect, 1) + " LIMIT 1"
	err := r.q.QueryRowContext(ctx, querySel, sessionID).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	queryIns := fmt.Sprintf(
		"INSERT INTO memori_conversation (uuid, session_id, date_created) VALUES (%s, %s, %s) ON CONFLICT DO NOTHING RETURNING id",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
	)
	var id int64
	err = r.q.QueryRowContext(ctx, queryIns, uuid.New().String(), sessionID, time.Now()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return r.GetBySessionID(ctx, sessionID)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *sqlConversationRepo) GetBySessionID(ctx context.Context, sessionID int64) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_conversation WHERE session_id = " +
		sqlPlaceholder(r.dialect, 1) + " ORDER BY date_created DESC LIMIT 1"
	err := r.q.QueryRowContext(ctx, query, sessionID).Scan(&id)
	if err != nil {
		return 0, mapSQLNotFound(err)
	}
	return id, nil
}

func (r *sqlConversationRepo) UpdateSummary(ctx context.Context, conversationID int64, summary string) error {
	query := fmt.Sprintf(
		"UPDATE memori_conversation SET summary = %s, date_updated = %s WHERE id = %s",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
	)
	_, err := r.q.ExecContext(ctx, query, summary, time.Now(), conversationID)
	return err
}

func (r *sqlConversationRepo) GetSummary(ctx context.Context, conversationID int64) (string, error) {
	var summary sql.NullString
	query := "SELECT summary FROM memori_conversation WHERE id = " + sqlPlaceholder(r.dialect, 1)
	if err := r.q.QueryRowContext(ctx, query, conversationID).Scan(&summary); err != nil {
		return "", mapSQLNotFound(err)
	}
	return summary.String, nil
}

type sqlMessageRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlMessageRepo) Create(ctx context.Context, conversationID int64, role, msgType, content string) error {
	query := fmt.Sprintf(
		`INSERT INTO memori_conversation_message
		 (uuid, conversation_id, role, type, content, date_created)
		 VALUES (%s, %s, %s, %s, %s, %s)`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
		sqlPlaceholder(r.dialect, 4), sqlPlaceholder(r.dialect, 5), sqlPlaceholder(r.dialect, 6),
	)
	_, err := r.q.ExecContext(ctx, query, uuid.New().String(), conversationID, role, msgType, content, time.Now())
	return err
}

func (r *sqlMessageRepo) Recent(ctx context.Context, conversationID int64, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	query := fmt.Sprintf(
		`SELECT id, conversation_id, role, content, date_created
		 FROM memori_conversation_message WHERE conversation_id = %s
		 ORDER BY id DESC LIMIT %s`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
	)
	rows, err := r.q.QueryContext(ctx, query, conversationID, limit)
	if err != nil {
		return nil, err
	}
	out, err := scanStoredMessages(rows, limit)
	if err != nil {
		return nil, err
	}
	reverseMessages(out)
	return out, nil
}

func (r *sqlMessageRepo) RecentForEntity(ctx context.Context, entityID int64, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	query := fmt.Sprintf(`
SELECT m.id, m.conversation_id, m.role, m.content, m.date_created
FROM memori_conversation_message m
JOIN memori_conversation c ON c.id = m.conversation_id
JOIN memori_session s ON s.id = c.session_id
WHERE s.entity_id = %s
ORDER BY m.id DESC
LIMIT %s`, sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2))
	rows, err := r.q.QueryContext(ctx, query, entityID, limit)
	if err != nil {
		return nil, err
	}
	out, err := scanStoredMessages(rows, limit)
	if err != nil {
		return nil, err
	}
	reverseMessages(out)
	return out, nil
}

func scanStoredMessages(rows *sql.Rows, capacity int) ([]StoredMessage, error) {
	defer rows.Close()
	out := make([]StoredMessage, 0, capacity)
	for rows.Next() {
		var m StoredMessage
		var created any
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &created); err != nil {
			return nil, err
		}
		if t, ok := decodeAnyTime(created); ok {
			m.DateCreated = t
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sqlMessageRepo) LatestUserMessageID(ctx context.Context, conversationID int64) (int64, bool, error) {
	query := fmt.Sprintf(
		`SELECT id FROM memori_conversation_message
		 WHERE conversation_id = %s AND role = 'user'
		 ORDER BY id DESC LIMIT 1`,
		sqlPlaceholder(r.dialect, 1),
	)
	var id int64
	err := r.q.QueryRowContext(ctx, query, conversationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func reverseMessages(msgs []StoredMessage) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

type sqlEntityFactRepo struct {
	q       sqlQuerier
	dialect string
}

func (r *sqlEntityFactRepo) Upsert(ctx context.Context, entityID int64, in FactInput) (int64, error) {
	ph := func(n int) string { return sqlPlaceholder(r.dialect, n) }
	query := fmt.Sprintf(`
INSERT INTO memori_entity_fact
 (uuid, entity_id, content, content_embedding, num_times, date_last_time,
  uniq, slot, embedding_model, embedding_dim, source_conversation_id, status, date_created, search_text,
  valid_from, source_message_id, confidence, asserted_by)
VALUES (%s, %s, %s, %s, 1, %s, %s, %s, %s, %s, %s, 'active', %s, %s, %s, %s, %s, %s)
ON CONFLICT(entity_id, uniq) DO UPDATE SET
 content = excluded.content,
 content_embedding = excluded.content_embedding,
 num_times = memori_entity_fact.num_times + 1,
 date_last_time = excluded.date_last_time,
 date_updated = excluded.date_created,
 slot = excluded.slot,
 embedding_model = excluded.embedding_model,
 embedding_dim = excluded.embedding_dim,
 source_conversation_id = COALESCE(excluded.source_conversation_id, memori_entity_fact.source_conversation_id),
 source_message_id = COALESCE(excluded.source_message_id, memori_entity_fact.source_message_id),
 search_text = excluded.search_text,
 confidence = excluded.confidence,
 asserted_by = excluded.asserted_by,
 valid_from = CASE
   WHEN memori_entity_fact.status IN ('superseded', 'deleted', 'expired') AND NOT %s
   THEN excluded.valid_from
   ELSE COALESCE(memori_entity_fact.valid_from, excluded.valid_from)
 END,
 valid_to = CASE
   WHEN memori_entity_fact.status IN ('superseded', 'deleted', 'expired') AND %s
   THEN memori_entity_fact.valid_to
   ELSE NULL
 END,
 status = CASE
   WHEN memori_entity_fact.status IN ('superseded', 'deleted', 'expired') AND %s
   THEN memori_entity_fact.status
   ELSE 'active'
 END
RETURNING id`,
		ph(1), ph(2), ph(3), ph(4), ph(5), ph(6), ph(7), ph(8), ph(9), ph(10),
		ph(11), ph(12), ph(13), ph(14), ph(15), ph(16), ph(17), ph(18), ph(19),
	)

	var id int64
	now := time.Now()
	assertedBy, confidence := normalizeAssertion(in.AssertedBy, in.Confidence)
	err := r.q.QueryRowContext(ctx, query,
		uuid.New().String(), entityID, in.Content, in.Embedding,
		now, in.Uniq, in.Slot, in.EmbeddingModel, in.EmbeddingDim,
		nullableInt64(in.SourceConversationID), now, storedSearchText(in.Content),
		now, nullableInt64(in.SourceMessageID), confidence, assertedBy,
		in.PreserveInactive, in.PreserveInactive, in.PreserveInactive,
	).Scan(&id)
	return id, err
}

func (r *sqlEntityFactRepo) Touch(ctx context.Context, factID int64) error {
	query := fmt.Sprintf(
		"UPDATE memori_entity_fact SET num_times = num_times + 1, date_last_time = %s, date_updated = %s WHERE id = %s AND status = 'active'",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
	)
	now := time.Now()
	_, err := r.q.ExecContext(ctx, query, now, now, factID)
	return err
}

func (r *sqlEntityFactRepo) HasInactiveUniq(ctx context.Context, entityID int64, uniq string) (bool, error) {
	query := fmt.Sprintf(
		"SELECT 1 FROM memori_entity_fact WHERE entity_id = %s AND uniq = %s AND status IN ('superseded', 'deleted', 'expired') LIMIT 1",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
	)
	var one int
	err := r.q.QueryRowContext(ctx, query, entityID, uniq).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *sqlEntityFactRepo) SupersedeSlot(ctx context.Context, entityID int64, slot string, exceptID int64) error {
	if slot == "" {
		return nil
	}
	query := fmt.Sprintf(
		`UPDATE memori_entity_fact
		 SET status = 'superseded', valid_to = %s, date_updated = %s
		 WHERE entity_id = %s AND slot = %s AND id <> %s AND status = 'active'`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
		sqlPlaceholder(r.dialect, 3), sqlPlaceholder(r.dialect, 4), sqlPlaceholder(r.dialect, 5),
	)
	now := time.Now()
	_, err := r.q.ExecContext(ctx, query, now, now, entityID, slot, exceptID)
	return err
}

func (r *sqlEntityFactRepo) scanFacts(rows *sql.Rows) ([]FactResult, error) {
	defer rows.Close()
	out := make([]FactResult, 0, 64)
	for rows.Next() {
		var f FactResult
		var embedding []byte
		var dateLast, validFrom, validTo any
		var sourceConv, sourceMsg sql.NullInt64
		var confidence sql.NullFloat64
		var assertedBy sql.NullString

		if err := rows.Scan(
			&f.ID, &f.Content, &embedding, &f.NumTimes, &dateLast,
			&f.Slot, &f.EmbeddingDim, &f.EmbeddingModel, &sourceConv,
			&validFrom, &validTo, &sourceMsg, &confidence, &assertedBy,
		); err != nil {
			return nil, err
		}
		if t, ok := decodeAnyTime(dateLast); ok {
			f.DateLastTime = t
		}
		if t, ok := decodeAnyTime(validFrom); ok {
			f.ValidFrom = t
		}
		if t, ok := decodeAnyTime(validTo); ok {
			f.ValidTo = &t
		}
		f.Embedding = DecodeEmbedding(embedding)
		if sourceConv.Valid {
			id := sourceConv.Int64
			f.SourceConversationID = &id
		}
		if sourceMsg.Valid {
			id := sourceMsg.Int64
			f.SourceMessageID = &id
		}
		f.Confidence = 1
		if confidence.Valid && confidence.Float64 > 0 {
			f.Confidence = confidence.Float64
		}
		f.AssertedBy = "user"
		if assertedBy.Valid && assertedBy.String != "" {
			f.AssertedBy = assertedBy.String
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sqlEntityFactRepo) Candidates(ctx context.Context, entityID int64, scanLimit int) ([]FactResult, error) {
	if scanLimit <= 0 {
		scanLimit = 2000
	}
	query := fmt.Sprintf(`
SELECT `+factColumns("")+`
FROM memori_entity_fact
WHERE entity_id = %s AND status = 'active'
ORDER BY date_last_time DESC, id DESC
LIMIT %s`, sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2))

	rows, err := r.q.QueryContext(ctx, query, entityID, scanLimit)
	if err != nil {
		return nil, err
	}
	return r.scanFacts(rows)
}

func (r *sqlEntityFactRepo) SearchLexical(ctx context.Context, entityID int64, query string, limit int) ([]FactResult, error) {
	if limit <= 0 {
		limit = 150
	}
	if limit > 500 {
		limit = 500
	}
	switch r.dialect {
	case "postgres":
		match, ok := postgresOrQuery(query)
		if !ok {
			return nil, nil
		}
		stmt := fmt.Sprintf(`
SELECT `+factColumns("")+`
FROM memori_entity_fact
WHERE entity_id = %s AND status = 'active'
  AND to_tsvector('simple', search_text) @@ to_tsquery('simple', %s)
ORDER BY ts_rank(to_tsvector('simple', search_text), to_tsquery('simple', %s)) DESC, id DESC
LIMIT %s`,
			sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
			sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
		)
		// PostgreSQL allows a placeholder to be reused. SQLite does not, and
		// this branch never runs there.
		rows, err := r.q.QueryContext(ctx, stmt, entityID, match, limit)
		if err != nil {
			return nil, err
		}
		return r.scanFacts(rows)
	default:
		match, ok := sqliteOrQuery(query)
		if !ok {
			return nil, nil
		}
		stmt := fmt.Sprintf(`
SELECT `+factColumns("f")+`
FROM memori_fact_fts
JOIN memori_entity_fact f ON f.id = memori_fact_fts.rowid
WHERE memori_fact_fts MATCH %s AND f.entity_id = %s AND f.status = 'active'
ORDER BY bm25(memori_fact_fts), f.id DESC
LIMIT %s`,
			sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
		)
		rows, err := r.q.QueryContext(ctx, stmt, match, entityID, limit)
		if err != nil {
			return nil, err
		}
		return r.scanFacts(rows)
	}
}

func factColumns(prefix string) string {
	col := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	return fmt.Sprintf(`%s, %s, %s, %s, %s,
       COALESCE(%s, ''), COALESCE(%s, 0), COALESCE(%s, ''),
       %s, %s, %s, %s,
       COALESCE(%s, 1), COALESCE(%s, 'user')`,
		col("id"), col("content"), col("content_embedding"), col("num_times"), col("date_last_time"),
		col("slot"), col("embedding_dim"), col("embedding_model"),
		col("source_conversation_id"), col("valid_from"), col("valid_to"), col("source_message_id"),
		col("confidence"), col("asserted_by"))
}

func normalizeAssertion(assertedBy string, confidence float64) (string, float64) {
	switch strings.ToLower(strings.TrimSpace(assertedBy)) {
	case "assistant":
		assertedBy = "assistant"
	case "inferred":
		assertedBy = "inferred"
	default:
		assertedBy = "user"
	}
	if confidence <= 0 || confidence > 1 {
		confidence = 1
	}
	return assertedBy, confidence
}

func storedSearchText(content string) string {
	doc := textutil.SearchDocument(content)
	if doc == "" {
		return " "
	}
	return doc
}

func sqliteOrQuery(query string) (string, bool) {
	tokens := textutil.SearchTokens(query)
	if len(tokens) == 0 {
		return "", false
	}
	if len(tokens) > 32 {
		tokens = tokens[:32]
	}
	parts := make([]string, len(tokens))
	for i, token := range tokens {
		parts[i] = `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
	}
	return strings.Join(parts, " OR "), true
}

func postgresOrQuery(query string) (string, bool) {
	tokens := textutil.SearchTokens(query)
	if len(tokens) == 0 {
		return "", false
	}
	if len(tokens) > 32 {
		tokens = tokens[:32]
	}
	parts := make([]string, len(tokens))
	for i, token := range tokens {
		parts[i] = "'" + strings.ReplaceAll(token, "'", "''") + "'"
	}
	return strings.Join(parts, " | "), true
}

func (r *sqlEntityFactRepo) List(ctx context.Context, entityID int64, limit int) ([]FactResult, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf(`
SELECT `+factColumns("")+`
FROM memori_entity_fact
WHERE entity_id = %s AND status = 'active'
ORDER BY date_last_time DESC, num_times DESC, id DESC
LIMIT %s`, sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2))

	rows, err := r.q.QueryContext(ctx, query, entityID, limit)
	if err != nil {
		return nil, err
	}
	return r.scanFacts(rows)
}

func (r *sqlEntityFactRepo) Forget(ctx context.Context, entityID int64, factID int64) error {
	query := fmt.Sprintf(
		"UPDATE memori_entity_fact SET status = 'deleted', valid_to = COALESCE(valid_to, %s), date_updated = %s WHERE entity_id = %s AND id = %s",
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3), sqlPlaceholder(r.dialect, 4),
	)
	now := time.Now()
	_, err := r.q.ExecContext(ctx, query, now, now, entityID, factID)
	return err
}

func (r *sqlEntityFactRepo) Expirable(ctx context.Context, entityID int64, cutoff time.Time, protectTimes int64) ([]int64, error) {
	query := fmt.Sprintf(
		`SELECT id FROM memori_entity_fact
		 WHERE entity_id = %s AND status = 'active' AND date_last_time < %s`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
	)
	args := []any{entityID, cutoff}
	if protectTimes > 0 {
		query += " AND num_times < " + sqlPlaceholder(r.dialect, 3)
		args = append(args, protectTimes)
	}
	query += " ORDER BY id"
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *sqlEntityFactRepo) Expire(ctx context.Context, entityID int64, factIDs []int64, at time.Time) error {
	if len(factIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(factIDs))
	args := make([]any, 0, len(factIDs)+3)
	args = append(args, at, at, entityID)
	for i, id := range factIDs {
		placeholders[i] = sqlPlaceholder(r.dialect, i+4)
		args = append(args, id)
	}
	query := fmt.Sprintf(
		`UPDATE memori_entity_fact
		 SET status = 'expired', valid_to = COALESCE(valid_to, %s), date_updated = %s
		 WHERE entity_id = %s AND status = 'active' AND id IN (%s)`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
		strings.Join(placeholders, ", "),
	)
	_, err := r.q.ExecContext(ctx, query, args...)
	return err
}

func (r *sqlEntityFactRepo) Restore(ctx context.Context, entityID int64, factID int64) error {
	query := fmt.Sprintf(
		`UPDATE memori_entity_fact
		 SET status = 'active', valid_to = NULL, date_updated = %s
		 WHERE entity_id = %s AND id = %s AND status = 'expired'`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2), sqlPlaceholder(r.dialect, 3),
	)
	res, err := r.q.ExecContext(ctx, query, time.Now(), entityID, factID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sqlEntityFactRepo) Count(ctx context.Context, entityID int64) (int64, int64, error) {
	query := fmt.Sprintf(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0)
		 FROM memori_entity_fact WHERE entity_id = %s`,
		sqlPlaceholder(r.dialect, 1),
	)
	var total, active int64
	err := r.q.QueryRowContext(ctx, query, entityID).Scan(&total, &active)
	return total, active, err
}

func (r *sqlEntityFactRepo) ListForReembed(ctx context.Context, entityID int64, model string, dim int, limit int) ([]FactForReembed, error) {
	if limit <= 0 {
		limit = 32
	}
	query := fmt.Sprintf(`
SELECT id, content FROM memori_entity_fact
WHERE entity_id = %s AND status = 'active'
  AND (embedding_model IS NULL OR embedding_model <> %s
       OR embedding_dim IS NULL OR embedding_dim <> %s)
ORDER BY id
LIMIT %s`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
		sqlPlaceholder(r.dialect, 3), sqlPlaceholder(r.dialect, 4),
	)
	rows, err := r.q.QueryContext(ctx, query, entityID, model, dim, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FactForReembed
	for rows.Next() {
		var f FactForReembed
		if err := rows.Scan(&f.ID, &f.Content); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *sqlEntityFactRepo) UpdateEmbedding(ctx context.Context, factID int64, embedding []byte, model string, dim int) error {
	query := fmt.Sprintf(
		`UPDATE memori_entity_fact
		 SET content_embedding = %s, embedding_model = %s, embedding_dim = %s, date_updated = %s
		 WHERE id = %s`,
		sqlPlaceholder(r.dialect, 1), sqlPlaceholder(r.dialect, 2),
		sqlPlaceholder(r.dialect, 3), sqlPlaceholder(r.dialect, 4), sqlPlaceholder(r.dialect, 5),
	)
	_, err := r.q.ExecContext(ctx, query, embedding, model, dim, time.Now(), factID)
	return err
}

// sqlReposBundle wires repository implementations against a shared querier.
type sqlReposBundle struct {
	entity       *sqlEntityRepo
	process      *sqlProcessRepo
	session      *sqlSessionRepo
	conversation *sqlConversationRepo
	message      *sqlMessageRepo
	entityFact   *sqlEntityFactRepo
}

func newSQLRepos(q sqlQuerier, dialect string) Repos {
	return &sqlReposBundle{
		entity:       &sqlEntityRepo{q: q, dialect: dialect},
		process:      &sqlProcessRepo{q: q, dialect: dialect},
		session:      &sqlSessionRepo{q: q, dialect: dialect},
		conversation: &sqlConversationRepo{q: q, dialect: dialect},
		message:      &sqlMessageRepo{q: q, dialect: dialect},
		entityFact:   &sqlEntityFactRepo{q: q, dialect: dialect},
	}
}

func (b *sqlReposBundle) Entity() EntityRepo             { return b.entity }
func (b *sqlReposBundle) Process() ProcessRepo           { return b.process }
func (b *sqlReposBundle) Session() SessionRepo           { return b.session }
func (b *sqlReposBundle) Conversation() ConversationRepo { return b.conversation }
func (b *sqlReposBundle) Message() MessageRepo           { return b.message }
func (b *sqlReposBundle) EntityFact() EntityFactRepo     { return b.entityFact }
