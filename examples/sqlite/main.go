package main

import (
	"context"
	"fmt"
	"time"

	"memorigo/memori"
)

func main() {
	db, cleanup := openInMemorySQLite()
	defer cleanup()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		panic(err)
	}

	// Attribution is required
	m.Attribution("user-123", "demo-bot")

	// Simulate a conversation (you can also call OpenAICompatClient and then persist)
	w := memori.NewWriter(m)
	payload := memori.ConversationPayload{
		Messages: []memori.Message{
			{Role: "user", Content: "My favorite color is blue"},
		},
		Response: &memori.Message{Role: "assistant", Content: "Noted."},
	}

	if err := w.Execute(context.Background(), payload); err != nil {
		panic(err)
	}

	// Wait a bit for async offline augmentation to upsert facts
	time.Sleep(50 * time.Millisecond)

	facts, err := m.Recall("favorite color", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f times=%d content=%q\n", i+1, f.Score, f.NumTimes, f.Content)
	}
}


