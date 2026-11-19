// Command adapter shows the framework-neutral Memorigo hook.
// A middleware in another framework would call adapter.Apply and send
// Result.Messages. This program does not import that framework, and it does
// not write the injected system message back into memory.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/adapter"
	"github.com/Deep-Octopus/Memorigo/memori"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	db, err := sql.Open("sqlite", "file:adapter_example?mode=memory&cache=shared")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.Embedding.Provider = "hash"
			c.Extraction.Provider = "heuristic"
			c.Recall.MinScore = 0.01
		}),
	)
	defer m.Close()
	if err := m.Err(); err != nil {
		return err
	}
	if err := m.TryAttribution("demo-user", "adapter-example"); err != nil {
		return err
	}
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		return err
	}

	original := []memori.ChatMessage{{Role: "user", Content: "what is my favorite color?"}}
	got, err := adapter.Apply(context.Background(), m, original, adapter.Options{Budget: 400})
	if err != nil {
		return err
	}
	if !got.Injected || len(got.Messages) != 2 || !strings.Contains(got.Messages[0].Content, "blue") {
		return fmt.Errorf("memory was not injected: %+v", got.Messages)
	}
	if len(original) != 1 {
		return fmt.Errorf("original request changed")
	}
	fmt.Println(got.Messages[0].Content)
	return nil
}
