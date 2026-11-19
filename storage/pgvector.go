package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ErrPGVectorRequired is returned when MEMORI_PGVECTOR=on and the Postgres
// vector extension is not installed. auto and off do not return this error.
// The driver does not install the extension.
var ErrPGVectorRequired = errors.New("storage: pgvector extension is not installed")

// SimilarFactSearcher is an optional candidate generator. SQLite and MongoDB
// do not implement it. A nil slice means the caller keeps the bounded scan.
type SimilarFactSearcher interface {
	SearchSimilar(ctx context.Context, entityID int64, embedding []float32, limit int) ([]FactResult, error)
}

type pgVectorDBState struct {
	mu        sync.Mutex
	probed    bool
	available bool
	dim       int
	warned    bool
}

var pgVectorStates sync.Map

func pgVectorMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MEMORI_PGVECTOR"))) {
	case "on", "true", "1", "yes":
		return "on"
	case "off", "false", "0", "no":
		return "off"
	default:
		return "auto"
	}
}

func (d *SQLDriver) requirePGVector() error {
	if d.dialect != "postgres" || pgVectorMode() != "on" {
		return nil
	}
	var present bool
	if err := d.db().QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return ErrPGVectorRequired
	}
	return nil
}

func (r *sqlEntityFactRepo) SearchSimilar(ctx context.Context, entityID int64, embedding []float32, limit int) ([]FactResult, error) {
	if r.dialect != "postgres" || pgVectorMode() == "off" || len(embedding) == 0 {
		return nil, nil
	}
	db, ok := sqlDB(r.q)
	if !ok {
		return nil, nil
	}
	ready, err := ensurePGVector(ctx, db, len(embedding))
	if err != nil {
		if pgVectorMode() != "on" {
			logPGVector(db, err)
			return nil, nil
		}
		return nil, err
	}
	if !ready {
		return nil, nil
	}
	if limit <= 0 {
		limit = 150
	}
	if limit > 500 {
		limit = 500
	}
	lit, err := vectorLiteral(embedding)
	if err != nil {
		if pgVectorMode() != "on" {
			logPGVector(db, err)
			return nil, nil
		}
		return nil, err
	}
	stmt := `
SELECT ` + factColumns("") + `
FROM memori_entity_fact
WHERE entity_id = $1 AND status = 'active' AND content_embedding_vec IS NOT NULL
ORDER BY content_embedding_vec <=> $2::vector
LIMIT $3`
	rows, err := r.q.QueryContext(ctx, stmt, entityID, lit, limit)
	if err != nil {
		if pgVectorMode() != "on" {
			logPGVector(db, err)
			return nil, nil
		}
		return nil, err
	}
	return r.scanFacts(rows)
}

func (r *sqlEntityFactRepo) syncPGVector(ctx context.Context, id int64, embedding []byte) error {
	if r.dialect != "postgres" || pgVectorMode() == "off" || len(embedding) == 0 || len(embedding)%4 != 0 {
		return nil
	}
	db, ok := sqlDB(r.q)
	if !ok {
		return nil
	}
	vec := DecodeEmbedding(embedding)
	ready, err := ensurePGVector(ctx, db, len(vec))
	if err != nil || !ready {
		if err != nil && pgVectorMode() != "on" {
			logPGVector(db, err)
			return nil
		}
		return err
	}
	lit, err := vectorLiteral(vec)
	if err != nil {
		if pgVectorMode() != "on" {
			logPGVector(db, err)
			return nil
		}
		return err
	}
	_, err = r.q.ExecContext(ctx, `UPDATE memori_entity_fact SET content_embedding_vec = $1::vector WHERE id = $2`, lit, id)
	if err != nil && pgVectorMode() != "on" {
		logPGVector(db, err)
		return nil
	}
	return err
}

func ensurePGVector(ctx context.Context, db *sql.DB, dim int) (bool, error) {
	state := pgVectorState(db)
	state.mu.Lock()
	defer state.mu.Unlock()

	mode := pgVectorMode()
	if mode == "off" {
		return false, nil
	}
	if err := probePGVector(ctx, db, state, mode); err != nil {
		return false, err
	}
	if !state.available {
		if mode == "on" {
			return false, ErrPGVectorRequired
		}
		return false, nil
	}
	if dim <= 0 {
		return false, nil
	}
	if state.dim != 0 && state.dim != dim {
		return false, pgVectorDimError(state, state.dim, dim, mode)
	}
	existing, err := pgVectorColumnDim(ctx, db)
	if err != nil {
		return false, err
	}
	if existing == 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(
			`ALTER TABLE memori_entity_fact ADD COLUMN IF NOT EXISTS content_embedding_vec vector(%d)`, dim)); err != nil {
			return false, err
		}
		if _, err := db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_memori_entity_fact_hnsw
ON memori_entity_fact USING hnsw (content_embedding_vec vector_cosine_ops)`); err != nil {
			return false, err
		}
		existing = dim
	}
	if existing != dim {
		state.dim = existing
		return false, pgVectorDimError(state, existing, dim, mode)
	}
	if err := backfillPGVector(ctx, db, dim); err != nil {
		return false, err
	}
	state.dim = dim
	return true, nil
}

func probePGVector(ctx context.Context, db *sql.DB, state *pgVectorDBState, mode string) error {
	if state.probed {
		return nil
	}
	var present bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&present); err != nil {
		return err
	}
	state.probed = true
	state.available = present
	if !present && mode == "auto" {
		warnPGVector(state, ErrPGVectorRequired)
	}
	return nil
}

func pgVectorDimError(state *pgVectorDBState, columnDim, embeddingDim int, mode string) error {
	err := fmt.Errorf("storage: pgvector column is vector(%d), embedding has %d dimensions", columnDim, embeddingDim)
	if mode != "on" {
		warnPGVector(state, err)
		return nil
	}
	return err
}

func pgVectorColumnDim(ctx context.Context, db *sql.DB) (int, error) {
	var formatted sql.NullString
	err := db.QueryRowContext(ctx, `
SELECT format_type(a.atttypid, a.atttypmod)
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = 'memori_entity_fact'
  AND a.attname = 'content_embedding_vec'
  AND a.attnum > 0
  AND NOT a.attisdropped
  AND n.nspname = current_schema()`).Scan(&formatted)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !formatted.Valid {
		return 0, nil
	}
	text := strings.ToLower(strings.TrimSpace(formatted.String))
	if !strings.HasPrefix(text, "vector(") || !strings.HasSuffix(text, ")") {
		return 0, fmt.Errorf("storage: unexpected pgvector type %q", formatted.String)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(text, "vector("), ")"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("storage: unexpected pgvector type %q", formatted.String)
	}
	return n, nil
}

func backfillPGVector(ctx context.Context, db *sql.DB, dim int) error {
	if rlsMode() == "on" {
		return bypassRLS(ctx, db, func(q stdSQL) error {
			return backfillPGVectorOn(ctx, q, dim)
		})
	}
	return backfillPGVectorOn(ctx, db, dim)
}

func backfillPGVectorOn(ctx context.Context, q stdSQL, dim int) error {
	for {
		rows, err := q.QueryContext(ctx, `
SELECT id, content_embedding
FROM memori_entity_fact
WHERE content_embedding_vec IS NULL
  AND embedding_dim = $1
  AND octet_length(content_embedding) = $2
LIMIT 200`, dim, dim*4)
		if err != nil {
			return err
		}
		type pending struct {
			id  int64
			raw []byte
		}
		var batch []pending
		for rows.Next() {
			var row pending
			if err := rows.Scan(&row.id, &row.raw); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			lit, err := vectorLiteral(DecodeEmbedding(row.raw))
			if err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `UPDATE memori_entity_fact SET content_embedding_vec = $1::vector WHERE id = $2`, lit, row.id); err != nil {
				return err
			}
		}
	}
}

func pgVectorState(db *sql.DB) *pgVectorDBState {
	value, _ := pgVectorStates.LoadOrStore(db, &pgVectorDBState{})
	return value.(*pgVectorDBState)
}

func logPGVector(db *sql.DB, err error) {
	if err == nil {
		return
	}
	state := pgVectorState(db)
	state.mu.Lock()
	defer state.mu.Unlock()
	warnPGVector(state, err)
}

// warnPGVector logs once. The caller already holds state.mu.
func warnPGVector(state *pgVectorDBState, err error) {
	if state.warned || err == nil {
		return
	}
	state.warned = true
	log.Printf("memorigo: %v; dense recall stays a bounded scan", err)
}

func vectorLiteral(v []float32) (string, error) {
	if len(v) == 0 {
		return "", fmt.Errorf("storage: empty embedding")
	}
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, value := range v {
		f := float64(value)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("storage: embedding is not finite")
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(f, 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}
