package memori_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"memorigo/memori"
)

func TestAcceptance_SQLite_MigrateWriteAugmentRecall(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memori_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		t.Fatalf("migrate/build: %v", err)
	}

	m.Attribution("user-123", "proc-abc")

	w := memori.NewWriter(m)
	ctx := context.Background()

	var payload memori.ConversationPayload
	payload.Messages = []memori.Message{
		{Role: "user", Content: "My favorite color is blue"},
	}
	payload.Response = &memori.Message{Role: "assistant", Content: "Got it."}

	if err := w.Execute(ctx, payload); err != nil {
		t.Fatalf("writer execute: %v", err)
	}

	// Wait for async augmentation to write entity facts
	deadline := time.Now().Add(2 * time.Second)
	for {
		facts, err := m.Recall("favorite color", 5)
		if err != nil {
			t.Fatalf("recall: %v", err)
		}
		if len(facts) > 0 {
			found := false
			for _, f := range facts {
				if f.Content == "My favorite color is blue" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("facts returned but expected content not found: %#v", facts)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for augmentation to write facts")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestAcceptance_SQLite_SkipsWithoutAttribution(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memori_test2?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		t.Fatalf("migrate/build: %v", err)
	}

	// no Attribution => Recall should return empty
	facts, err := m.Recall("anything", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected no facts without attribution, got: %#v", facts)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}


