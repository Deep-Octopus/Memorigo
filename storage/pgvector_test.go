package storage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestPGVectorMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "auto"},
		{in: "auto", want: "auto"},
		{in: "ON", want: "on"},
		{in: "true", want: "on"},
		{in: "1", want: "on"},
		{in: "yes", want: "on"},
		{in: "off", want: "off"},
		{in: "false", want: "off"},
		{in: "0", want: "off"},
		{in: "no", want: "off"},
		{in: "maybe", want: "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Setenv("MEMORI_PGVECTOR", tc.in)
			if got := pgVectorMode(); got != tc.want {
				t.Fatalf("pgVectorMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestVectorLiteral(t *testing.T) {
	got, err := vectorLiteral([]float32{1, 0.5, -2})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[1,0.5,-2]" {
		t.Fatalf("literal = %q", got)
	}
	if _, err := vectorLiteral(nil); err == nil {
		t.Fatal("empty embedding accepted")
	}
	if _, err := vectorLiteral([]float32{float32(math.NaN())}); err == nil {
		t.Fatal("NaN accepted")
	}
	if _, err := vectorLiteral([]float32{float32(math.Inf(1))}); err == nil {
		t.Fatal("infinity accepted")
	}
}

func TestMigrationsDoNotRequirePGVector(t *testing.T) {
	sets := map[string]map[int][]string{
		"sqlite":   sqliteMigrations,
		"postgres": postgresMigrations,
	}
	// A bare "vector" would match the existing to_tsvector lexical index.
	banned := []string{"hnsw", "create extension", "content_embedding_vec", "::vector", " vector("}
	for name, migrations := range sets {
		if maxSQLMigrationVersion(migrations) != 5 {
			t.Errorf("%s schema version = %d, want 5", name, maxSQLMigrationVersion(migrations))
		}
		for version, stmts := range migrations {
			for _, stmt := range stmts {
				lower := strings.ToLower(stmt)
				for _, word := range banned {
					if strings.Contains(lower, word) {
						t.Errorf("%s migration %d contains %q", name, version, word)
					}
				}
			}
		}
	}
}

func TestPGVectorSourceDoesNotInstallExtension(t *testing.T) {
	src, err := os.ReadFile("pgvector.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToUpper(src), []byte("CREATE EXTENSION")) {
		t.Fatal("pgvector.go must not install the extension")
	}
}

func TestSQLiteSearchSimilarNoop(t *testing.T) {
	t.Setenv("MEMORI_PGVECTOR", "on")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:pgvector_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
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
	columns := sqliteColumns(t, db, "memori_entity_fact")
	if columns["content_embedding_vec"] {
		t.Fatal("sqlite gained a vector column")
	}

	repos, ok := mgr.Driver().(Repos)
	if !ok {
		t.Fatal("driver does not implement Repos")
	}
	ctx := context.Background()
	entityID, err := repos.Entity().Create(ctx, "sqlite-user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repos.EntityFact().Upsert(ctx, entityID, FactInput{
		Content:        "sqlite fact",
		Embedding:      EncodeEmbedding([]float32{1, 0}),
		Uniq:           "sqlite-fact",
		EmbeddingModel: "test",
		EmbeddingDim:   2,
	}); err != nil {
		t.Fatal(err)
	}
	similar, ok := repos.EntityFact().(SimilarFactSearcher)
	if !ok {
		t.Fatal("sqlite fact repo does not implement SimilarFactSearcher")
	}
	hits, err := similar.SearchSimilar(ctx, entityID, []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if hits != nil {
		t.Fatalf("sqlite SearchSimilar = %#v, want nil", hits)
	}
	hits, err = similar.SearchSimilar(ctx, entityID, nil, 5)
	if err != nil || hits != nil {
		t.Fatalf("empty embedding: hits=%#v err=%v", hits, err)
	}
}
