package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"memorigo/memori"
)

func main() {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		// Example:
		// MONGODB_URI=mongodb://localhost:27017
		panic("MONGODB_URI is required")
	}
	dbName := os.Getenv("MONGODB_DB")
	if dbName == "" {
		dbName = "memori"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	db := client.Database(dbName)

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		panic(err)
	}

	m.Attribution("user-123", "demo-bot")

	w := memori.NewWriter(m)
	payload := memori.ConversationPayload{
		Messages: []memori.Message{
			{Role: "user", Content: "My favorite food is noodles"},
		},
		Response: &memori.Message{Role: "assistant", Content: "Nice."},
	}

	if err := w.Execute(context.Background(), payload); err != nil {
		panic(err)
	}

	time.Sleep(150 * time.Millisecond)

	facts, err := m.Recall("favorite food", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f times=%d content=%q\n", i+1, f.Score, f.NumTimes, f.Content)
	}
}


