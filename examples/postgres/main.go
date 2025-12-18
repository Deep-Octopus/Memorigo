package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"memorigo/memori"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		// Example:
		// POSTGRES_DSN=postgres://user:pass@localhost:5432/memori?sslmode=disable
		panic("POSTGRES_DSN is required")
	}

	// Use pgx stdlib driver so we can pass *sql.DB into memorigo.
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	m := memori.New(memori.WithStorageConn(db))
	if err := m.Storage.Build(); err != nil {
		panic(err)
	}

	m.Attribution("user-123", "demo-bot")

	w := memori.NewWriter(m)
	payload := memori.ConversationPayload{
		Messages: []memori.Message{
			{Role: "user", Content: "I live in Shanghai"},
		},
		Response: &memori.Message{Role: "assistant", Content: "Got it."},
	}

	if err := w.Execute(context.Background(), payload); err != nil {
		panic(err)
	}

	time.Sleep(100 * time.Millisecond)

	facts, err := m.Recall("Where do I live?", 5)
	if err != nil {
		panic(err)
	}

	fmt.Println("Recall results:")
	for i, f := range facts {
		fmt.Printf("%d) score=%.4f times=%d content=%q\n", i+1, f.Score, f.NumTimes, f.Content)
	}

}


