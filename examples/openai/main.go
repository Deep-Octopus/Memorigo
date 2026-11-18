package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
)

// Example: OpenAI + SQLite (in-memory) with automatic memory injection.
// Requires: OPENAI_API_KEY
func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("OPENAI_API_KEY not set; skipping OpenAI example")
		return
	}

	db, cleanup := openInMemorySQLite()
	defer cleanup()

	m := memori.New(memori.WithStorageConn(db))
	defer m.Close()

	if err := m.TryAttribution("user-openai", "demo-bot"); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Seed one long-term memory. In a real app this arrives through the writer
	// or Remember; recall during the next turn is automatic.
	if _, err := m.Remember("My favorite programming language is Go"); err != nil {
		panic(err)
	}

	m.OpenAI.Register(memori.NewOpenAIClient())
	memClient := m.OpenAIClient()

	req := memori.ChatCompletionsRequest{
		Model: "gpt-4o-mini",
		Messages: []memori.ChatMessage{
			{Role: "user", Content: "What is my favorite programming language, and why?"},
		},
	}

	resp, err := memClient.ChatCompletionsCreate(ctx, req)
	if err != nil {
		panic(err)
	}

	fmt.Println("LLM response:")
	if len(resp.Choices) > 0 {
		fmt.Println(resp.Choices[0].Message.Content)
	}

	// The wrapper persisted the original messages; wait for augmentation.
	if err := m.Flush(ctx); err != nil {
		panic(err)
	}

	facts, err := m.Recall("favorite programming language", 5)
	if err != nil {
		panic(err)
	}
	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f slot=%q content=%q\n", i+1, f.Score, f.Slot, f.Content)
	}
}
