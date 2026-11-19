package memori_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/storage"
)

func testDSN(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
	return fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", safe, time.Now().UnixNano())
}

func newTestMemori(t *testing.T, opts ...memori.Option) *memori.Memori {
	t.Helper()
	m, _ := newTestMemoriDB(t, opts...)
	return m
}

func newTestMemoriWithEnv(t *testing.T, env map[string]string, opts ...memori.Option) *memori.Memori {
	t.Helper()
	m, _ := newTestMemoriDBWithEnv(t, env, opts...)
	return m
}

func newTestMemoriDB(t *testing.T, opts ...memori.Option) (*memori.Memori, *sql.DB) {
	t.Helper()
	return newTestMemoriDBWithEnv(t, nil, opts...)
}

func newTestMemoriDBWithEnv(t *testing.T, env map[string]string, opts ...memori.Option) (*memori.Memori, *sql.DB) {
	t.Helper()
	t.Setenv("MEMORI_EMBEDDING_PROVIDER", "hash")
	t.Setenv("MEMORI_LOG_LEVEL", "off")
	t.Setenv("MEMORI_EXTRACTION_PROVIDER", "heuristic")
	t.Setenv("MEMORI_RECALL_MIN_SCORE", "0.01")
	t.Setenv("MEMORI_PROFILE_DYNAMIC", "false")
	t.Setenv("MEMORI_PROFILE_BUDGET", "200")
	t.Setenv("MEMORI_PROFILE_LIMIT", "40")
	for k, v := range env {
		t.Setenv(k, v)
	}

	db, err := sql.Open("sqlite", testDSN(t.Name()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)

	options := []memori.Option{memori.WithStorageConn(db)}
	options = append(options, opts...)
	m := memori.New(options...)
	if err := m.Err(); err != nil {
		t.Fatalf("memori init: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Close()
		_ = db.Close()
	})
	return m, db
}

func flushMemori(t *testing.T, m *memori.Memori) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func writeConversation(t *testing.T, m *memori.Memori, messages []memori.Message, response *memori.Message) {
	t.Helper()
	w := memori.NewWriter(m)
	payload := memori.ConversationPayload{Messages: messages, Response: response}
	if err := w.Execute(context.Background(), payload); err != nil {
		t.Fatalf("writer: %v", err)
	}
}

func currentConversationID(t *testing.T, m *memori.Memori) int64 {
	t.Helper()
	repos := m.Storage.Driver().(storage.Repos)
	ctx := context.Background()
	sessionID, err := repos.Session().GetByUUID(ctx, m.SessionID())
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	conversationID, err := repos.Conversation().GetBySessionID(ctx, sessionID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	return conversationID
}

func recentStoredMessages(t *testing.T, m *memori.Memori) []storage.StoredMessage {
	t.Helper()
	repos := m.Storage.Driver().(storage.Repos)
	msgs, err := repos.Message().Recent(context.Background(), currentConversationID(t, m), 200)
	if err != nil {
		t.Fatalf("recent messages: %v", err)
	}
	return msgs
}

func conversationSummary(t *testing.T, m *memori.Memori) string {
	t.Helper()
	repos := m.Storage.Driver().(storage.Repos)
	summary, err := repos.Conversation().GetSummary(context.Background(), currentConversationID(t, m))
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	return summary
}
