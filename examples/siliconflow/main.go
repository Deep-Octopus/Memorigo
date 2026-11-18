package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
)

// Example: SiliconFlow + SQLite (in-memory) with automatic memory injection.
// Requires: SILICONFLOW_API_KEY, optional SILICONFLOW_BASE_URL.
func main() {
	if os.Getenv("SILICONFLOW_API_KEY") == "" {
		fmt.Println("SILICONFLOW_API_KEY not set; skipping SiliconFlow example")
		return
	}

	db, cleanup := openInMemorySQLite()
	defer cleanup()

	m := memori.New(memori.WithStorageConn(db))
	defer m.Close()

	if err := m.TryAttribution("user-sf", "demo-bot"); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := m.Remember("My favorite city is Beijing"); err != nil {
		panic(err)
	}

	m.OpenAI.Register(memori.NewSiliconFlowClient())
	memClient := m.OpenAIClient()

	req := memori.ChatCompletionsRequest{
		Model: "Pro/Qwen/Qwen2.5-VL-7B-Instruct", // or another SiliconFlow model
		Messages: []memori.ChatMessage{
			{Role: "user", Content: "Which city do I like most?"},
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

	if err := m.Flush(ctx); err != nil {
		panic(err)
	}

	facts, err := m.Recall("favorite city", 5)
	if err != nil {
		panic(err)
	}
	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f slot=%q content=%q\n", i+1, f.Score, f.Slot, f.Content)
	}
}
