package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"memorigo/memori"
)

// Example: SiliconFlow + SQLite (in-memory) with one-line registration.
// Requires: SILICONFLOW_API_KEY, optional SILICONFLOW_BASE_URL (default https://api.siliconflow.cn)
func main() {
	if os.Getenv("SILICONFLOW_API_KEY") == "" {
		fmt.Println("SILICONFLOW_API_KEY not set; skipping SiliconFlow example")
		return
	}

	db, cleanup := openInMemorySQLite()
	defer cleanup()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		panic(err)
	}

	m.Attribution("user-sf", "demo-bot")

	client := memori.NewSiliconFlowClient()
	m.OpenAI.Register(client)

	memClient := m.OpenAIClient()

	req := memori.ChatCompletionsRequest{
		Model: "Pro/Qwen/Qwen2.5-VL-7B-Instruct", // or SiliconFlow-specific model name
		Messages: []memori.ChatMessage{
			{Role: "user", Content: "My favorite city is Beijing."},
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

	time.Sleep(100 * time.Millisecond)

	facts, err := m.Recall("favorite city", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f times=%d content=%q\n", i+1, f.Score, f.NumTimes, f.Content)
	}
}
