package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"memorigo/memori"
)

// Example: OpenAI + SQLite (in-memory) with one-line registration.
// Requires: OPENAI_API_KEY
func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("OPENAI_API_KEY not set; skipping OpenAI example")
		return
	}

	// For simplicity, use the same helper as sqlite example (copy/paste if needed).
	db, cleanup := openInMemorySQLite()
	defer cleanup()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		panic(err)
	}

	m.Attribution("user-openai", "demo-bot")

	// One-line registration
	client := memori.NewOpenAIClient()
	m.OpenAI.Register(client)

	// Use the memori-aware client so that calls are automatically persisted.
	memClient := m.OpenAIClient()

	req := memori.ChatCompletionsRequest{
		Model: "gpt-4o-mini",
		Messages: []memori.ChatMessage{
			{Role: "user", Content: "My favorite programming language is Go."},
		},
	}

	resp, err := memClient.ChatCompletionsCreate(context.Background(), req)
	if err != nil {
		panic(err)
	}

	fmt.Println("LLM response:")
	if len(resp.Choices) > 0 {
		fmt.Println(resp.Choices[0].Message.Content)
	}

	// Wait a bit for offline augmentation to persist facts.
	time.Sleep(100 * time.Millisecond)

	facts, err := m.Recall("favorite programming language", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f times=%d content=%q\n", i+1, f.Score, f.NumTimes, f.Content)
	}
}
