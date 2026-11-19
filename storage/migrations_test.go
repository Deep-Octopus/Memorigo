package storage

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLMigrationUpgradesVersion1(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:migrate_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// Build the v1 schema exactly as an early Memorigo release would have.
	for _, stmt := range sqliteMigrations[1] {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("v1 schema: %v", err)
		}
	}
	if _, err := db.Exec("INSERT INTO memori_schema_version(num) VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatalf("migrate v1 -> current: %v", err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatalf("migrate should be idempotent: %v", err)
	}

	var version int
	if err := db.QueryRow("SELECT num FROM memori_schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("schema version = %d, want 5", version)
	}

	columns := sqliteColumns(t, db, "memori_entity_fact")
	for _, want := range []string{
		"slot", "status", "embedding_model", "embedding_dim", "source_conversation_id", "search_text",
		"valid_from", "valid_to", "source_message_id", "confidence", "asserted_by",
	} {
		if _, ok := columns[want]; !ok {
			t.Errorf("missing column %q after migration", want)
		}
	}
	var fts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memori_fact_fts'`).Scan(&fts); err != nil {
		t.Fatal(err)
	}
	if fts != 1 {
		t.Fatal("fts table was not created")
	}
}

func sqliteColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
