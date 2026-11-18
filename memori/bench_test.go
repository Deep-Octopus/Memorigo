package memori_test

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func newBenchMemori(b *testing.B, facts int) *memori.Memori {
	b.Helper()
	dsn := fmt.Sprintf("file:bench_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	b.Cleanup(func() { _ = db.Close() })

	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.LogLevel = "off"
			c.AutoInject = false
			c.Embedding.Provider = "hash"
			c.Extraction.Provider = "heuristic"
		}),
	)
	b.Cleanup(func() { _ = m.Close() })
	if err := m.Err(); err != nil {
		b.Fatalf("memori: %v", err)
	}
	if err := m.TryAttribution("bench-user", "bench-process"); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < facts; i++ {
		if _, err := m.Remember(fmt.Sprintf(
			"The archived project note number %d records that codename code-%d belongs to team %d",
			i, i, i%17,
		)); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}
	runtime.GC()
	return m
}

func BenchmarkRecall1000(b *testing.B) {
	m := newBenchMemori(b, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Recall("project codename code-777", 5); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildContext1000(b *testing.B) {
	m := newBenchMemori(b, 1000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkg, err := m.BuildContext(ctx, "project codename code-777", memori.ContextOptions{TokenBudget: 800})
		if err != nil {
			b.Fatal(err)
		}
		if pkg.Tokens == 0 {
			b.Fatal("empty context")
		}
	}
}
