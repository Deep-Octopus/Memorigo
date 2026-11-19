package adapter_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/adapter"
	"github.com/Deep-Octopus/Memorigo/memori"
)

var dbCounter atomic.Int64

func newMemori(t *testing.T) *memori.Memori {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:adapter_%d?mode=memory&cache=shared", dbCounter.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.LogLevel = "off"
			c.AutoInject = false
			c.Embedding.Provider = "hash"
			c.Extraction.Provider = "heuristic"
			c.Recall.MinScore = 0.01
		}),
	)
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Err(); err != nil {
		t.Fatal(err)
	}
	if err := m.TryAttribution("adapter-user", "adapter-test"); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApplyInsertsMemoryWithoutStoringIt(t *testing.T) {
	m := newMemori(t)
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	before, err := m.Export(memori.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input := []memori.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "what is my favorite color?"},
	}
	got, err := adapter.Apply(context.Background(), m, input, adapter.Options{Budget: 400})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Injected {
		t.Fatal("expected an injected system message")
	}
	if len(input) != 2 {
		t.Fatalf("input was modified: %+v", input)
	}
	if len(got.Messages) != 3 || got.Messages[0].Content != "You are helpful." {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if got.Messages[1].Role != "system" || !strings.Contains(got.Messages[1].Content, "blue") || !strings.Contains(got.Messages[1].Content, adapter.Marker) {
		t.Fatalf("memory message = %+v", got.Messages[1])
	}
	if got.Messages[2].Role != "user" {
		t.Fatalf("user message moved: %+v", got.Messages)
	}
	again, err := adapter.Apply(context.Background(), m, got.Messages, adapter.Options{Budget: 400})
	if err != nil {
		t.Fatal(err)
	}
	if again.Injected || len(again.Messages) != 3 {
		t.Fatalf("second apply = injected %v len %d", again.Injected, len(again.Messages))
	}
	after, err := m.Export(memori.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Messages) != len(before.Messages) || len(after.Facts) != len(before.Facts) {
		t.Fatalf("apply stored memory: facts %d->%d messages %d->%d",
			len(before.Facts), len(after.Facts), len(before.Messages), len(after.Messages))
	}
}

func TestApplySkipsEmptyAndNil(t *testing.T) {
	m := newMemori(t)
	got, err := adapter.Apply(context.Background(), m, []memori.ChatMessage{
		{Role: "assistant", Content: "hello"},
	}, adapter.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Injected {
		t.Fatal("assistant-only turn should not inject")
	}
	if _, err := adapter.Apply(context.Background(), nil, nil, adapter.Options{}); err == nil {
		t.Fatal("expected nil memori error")
	}
}
