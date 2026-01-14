package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"memorigo/memori"
	_ "modernc.org/sqlite"
)

// openInMemorySQLite creates an in-memory SQLite database for testing
func openInMemorySQLite() (*sql.DB, func()) {
	db, err := sql.Open("sqlite", "file:embedding_demo?mode=memory&cache=shared")
	if err != nil {
		panic(err)
	}
	cleanup := func() { _ = db.Close() }
	return db, cleanup
}

func main() {
	fmt.Println("=== Memori Embedding Demo ===")

	// Example 1: Using Hash embedding (default, offline)
	fmt.Println("\n1. Using Hash embedding (offline, default):")
	runEmbeddingExample("hash", "", "")

	// Example 2: Using OpenAI embedding (requires OPENAI_API_KEY)
	if os.Getenv("OPENAI_API_KEY") != "" {
		fmt.Println("\n2. Using OpenAI embedding:")
		runEmbeddingExample("openai", os.Getenv("OPENAI_API_KEY"), "")
	} else {
		fmt.Println("\n2. OpenAI embedding skipped (set OPENAI_API_KEY to test)")
	}

	// Example 3: Using SiliconFlow embedding (requires SILICONFLOW_API_KEY)
	if os.Getenv("SILICONFLOW_API_KEY") != "" {
		fmt.Println("\n3. Using SiliconFlow embedding:")
		runEmbeddingExample("siliconflow", os.Getenv("SILICONFLOW_API_KEY"), "")
	} else {
		fmt.Println("\n3. SiliconFlow embedding skipped (set SILICONFLOW_API_KEY to test)")
	}
}

func runEmbeddingExample(provider, apiKey, baseURL string) {
	// Set environment variables for this example
	os.Setenv("MEMORI_EMBEDDING_PROVIDER", provider)
	if apiKey != "" {
		os.Setenv("MEMORI_EMBEDDING_API_KEY", apiKey)
	}
	if baseURL != "" {
		os.Setenv("MEMORI_EMBEDDING_BASE_URL", baseURL)
	}

	// Create in-memory SQLite for testing
	db, cleanup := openInMemorySQLite()
	defer cleanup()

	// Create Memori instance with storage
	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		fmt.Printf("Failed to build storage: %v\n", err)
		return
	}

	// Show embedding info
	fmt.Printf("Embedding provider: %s\n", m.Embedder.Provider())
	fmt.Printf("Embedding dimension: %d\n", m.Embedder.Dimension())

	// Test embedding functionality
	testTexts := []string{
		"My favorite color is blue",
		"I love programming in Go",
		"The weather is nice today",
	}

	fmt.Println("Testing embedding generation:")
	for _, text := range testTexts {
		embedding, err := m.Embedder.EmbedText(context.Background(), text)
		if err != nil {
			fmt.Printf("Error embedding '%s': %v\n", text, err)
			continue
		}
		fmt.Printf("✓ Embedded: '%s' -> %d dimensions\n", text, len(embedding))
	}

	// Test batch embedding
	fmt.Println("\nTesting batch embedding:")
	embeddings, err := m.Embedder.EmbedTexts(context.Background(), testTexts)
	if err != nil {
		fmt.Printf("Error batch embedding: %v\n", err)
		return
	}
	fmt.Printf("✓ Batch embedded %d texts\n", len(embeddings))

	// Test similarity calculation
	fmt.Println("\nTesting similarity calculation:")
	if len(embeddings) >= 2 {
		similarity := memori.CosineSimilarity(embeddings[0], embeddings[1])
		fmt.Printf("Similarity between '%s' and '%s': %.4f\n",
			testTexts[0], testTexts[1], similarity)
	}

	// Test with actual conversation and recall
	fmt.Println("\nTesting with conversation and recall:")
	m.Attribution("test-user", "embedding-demo")

	// Simulate conversation
	writer := memori.NewWriter(m)
	payload := memori.ConversationPayload{
		Messages: []memori.Message{
			{Role: "user", Content: "My favorite programming language is Go"},
			{Role: "user", Content: "I also like Python for data science"},
		},
		Response: &memori.Message{Role: "assistant", Content: "That's great! Both are excellent languages."},
	}

	if err := writer.Execute(context.Background(), payload); err != nil {
		fmt.Printf("Error writing conversation: %v\n", err)
		return
	}

	// Wait for augmentation
	time.Sleep(100 * time.Millisecond)

	// Test recall
	facts, err := m.Recall("programming language", 5)
	if err != nil {
		fmt.Printf("Error recalling facts: %v\n", err)
		return
	}

	fmt.Printf("Found %d relevant facts:\n", len(facts))
	for i, fact := range facts {
		fmt.Printf("  %d) Score: %.4f, Content: %q\n", i+1, fact.Score, fact.Content)
	}
}
