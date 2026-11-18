package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func main() {
	db, cleanup := openInMemorySQLite()
	defer cleanup()

	// Migrations run automatically when a connection is supplied.
	m := memori.New(memori.WithStorageConn(db))
	defer m.Close()

	if err := m.TryAttribution("user-123", "demo-bot"); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate a conversation.
	w := memori.NewWriter(m)
	payload := memori.ConversationPayload{
		Messages: []memori.Message{
			{Role: "user", Content: "My favorite color is blue"},
			{Role: "user", Content: "I live in Shanghai"},
		},
		Response: &memori.Message{Role: "assistant", Content: "Noted."},
	}
	if err := w.Execute(ctx, payload); err != nil {
		panic(err)
	}

	// Wait for asynchronous extraction, embedding and reconciliation.
	if err := m.Flush(ctx); err != nil {
		panic(err)
	}

	facts, err := m.Recall("favorite color", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f similarity=%.4f times=%d slot=%q content=%q\n",
			i+1, f.Score, f.Similarity, f.NumTimes, f.Slot, f.Content)
	}
}
