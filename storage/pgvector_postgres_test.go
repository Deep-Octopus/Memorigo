package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPGVectorPostgresScratchSchema talks to Postgres only when
// MEMORI_PGVECTOR_TEST_DSN is set. It never reads POSTGRES_DSN. The test
// creates a schema and drops it. go test ./... passes without this variable,
// without the vector extension, and without a network connection.
func TestPGVectorPostgresScratchSchema(t *testing.T) {
	dsn := os.Getenv("MEMORI_PGVECTOR_TEST_DSN")
	if dsn == "" {
		t.Skip("MEMORI_PGVECTOR_TEST_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("memorigo_pgv_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	}()
	// public stays on the path so an extension installed there still resolves.
	if _, err := db.Exec("SET search_path TO " + schema + ", public"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MEMORI_PGVECTOR", "auto")
	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow("SELECT num FROM memori_schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("schema version = %d, want 5", version)
	}

	var present bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	repos, ok := mgr.Driver().(Repos)
	if !ok {
		t.Fatal("driver does not implement Repos")
	}
	ctx := context.Background()
	entityID, err := repos.Entity().Create(ctx, "pgv-user")
	if err != nil {
		t.Fatal(err)
	}
	similar, ok := repos.EntityFact().(SimilarFactSearcher)
	if !ok {
		t.Fatal("postgres fact repo must implement SimilarFactSearcher")
	}

	if !present {
		if _, err := repos.EntityFact().Upsert(ctx, entityID, FactInput{
			Content:        "still scanned",
			Embedding:      EncodeEmbedding([]float32{1, 0}),
			Uniq:           "scanned",
			EmbeddingModel: "test",
			EmbeddingDim:   2,
		}); err != nil {
			t.Fatal(err)
		}
		hits, err := similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5)
		if err != nil || hits != nil {
			t.Fatalf("auto without extension: hits=%#v err=%v", hits, err)
		}
		var cols int
		if err := db.QueryRow(`
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'memori_entity_fact'
  AND column_name = 'content_embedding_vec'`).Scan(&cols); err != nil {
			t.Fatal(err)
		}
		if cols != 0 {
			t.Fatal("vector column created without the extension")
		}
		facts, err := repos.EntityFact().Candidates(ctx, entityID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(facts) != 1 || facts[0].Content != "still scanned" {
			t.Fatalf("candidates = %#v", facts)
		}
		t.Setenv("MEMORI_PGVECTOR", "on")
		if _, err := similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5); !errors.Is(err, ErrPGVectorRequired) {
			t.Fatalf("on without extension: %v", err)
		}
		if err := mgr.Build(); !errors.Is(err, ErrPGVectorRequired) {
			t.Fatalf("migrate on without extension: %v", err)
		}
		return
	}

	for _, fact := range []FactInput{
		{Content: "near", Embedding: EncodeEmbedding([]float32{1, 0}), Uniq: "near", EmbeddingModel: "test", EmbeddingDim: 2},
		{Content: "far", Embedding: EncodeEmbedding([]float32{0, 1}), Uniq: "far", EmbeddingModel: "test", EmbeddingDim: 2},
	} {
		if _, err := repos.EntityFact().Upsert(ctx, entityID, fact); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 || hits[0].Content != "near" {
		t.Fatalf("nearest hits = %#v", hits)
	}

	t.Setenv("MEMORI_PGVECTOR", "off")
	hits, err = similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5)
	if err != nil || hits != nil {
		t.Fatalf("off: hits=%#v err=%v", hits, err)
	}

	t.Setenv("MEMORI_PGVECTOR", "on")
	hits, err = similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5)
	if err != nil || len(hits) == 0 || hits[0].Content != "near" {
		t.Fatalf("on with extension: hits=%#v err=%v", hits, err)
	}
	_, err = similar.SearchSimilar(ctx, entityID, []float32{1, 0, 0}, 5)
	if err == nil || !strings.Contains(err.Error(), "vector(2)") {
		t.Fatalf("dimension mismatch: %v", err)
	}
}
