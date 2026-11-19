package mcpserver_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/mcpserver"
	"github.com/Deep-Octopus/Memorigo/memori"
)

var dbCounter atomic.Int64

func newMemori(t *testing.T) *memori.Memori {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:mcp_test_%d?mode=memory&cache=shared", dbCounter.Add(1)))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.LogLevel = "off"
			c.AutoInject = false
			c.Embedding.Provider = "hash"
			c.Extraction.Provider = "heuristic"
			c.Profile.Budget = 200
			c.Profile.Limit = 40
			c.Profile.IncludeDynamic = false
		}),
	)
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Err(); err != nil {
		t.Fatalf("memori: %v", err)
	}
	return m
}

func newSession(t *testing.T, readOnly bool) *mcp.ClientSession {
	t.Helper()
	server := mcpserver.New(newMemori(t), mcpserver.Options{ReadOnly: readOnly})
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "memorigo-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, error) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	if res.IsError {
		return b.String(), fmt.Errorf("tool error: %s", b.String())
	}
	return b.String(), nil
}

func TestToolsReadWriteFlow(t *testing.T) {
	session := newSession(t, false)

	raw, err := callTool(t, session, "memory_remember", map[string]any{
		"content": "My favorite color is blue",
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	var remembered struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &remembered); err != nil {
		t.Fatalf("decode remember result %q: %v", raw, err)
	}
	if remembered.ID == 0 {
		t.Fatalf("remember returned no id: %s", raw)
	}

	raw, err = callTool(t, session, "memory_search", map[string]any{
		"query": "favorite color",
		"limit": 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("search did not return the fact: %s", raw)
	}

	raw, err = callTool(t, session, "memory_context", map[string]any{
		"query":  "favorite color",
		"budget": 120,
	})
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if !strings.Contains(raw, "<memory_context>") || !strings.Contains(raw, "[fact:") {
		t.Fatalf("unexpected context package: %s", raw)
	}
	var contextResult struct {
		ProfileCount int    `json:"profile_count"`
		ProfileText  string `json:"profile_text"`
		Text         string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &contextResult); err != nil {
		t.Fatalf("decode context result: %v", err)
	}
	if contextResult.ProfileCount != 1 || !strings.Contains(contextResult.ProfileText, "blue") {
		t.Fatalf("memory_context did not include the profile: %s", raw)
	}

	raw, err = callTool(t, session, "memory_profile", map[string]any{"limit": 10})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if !strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("profile missing fact: %s", raw)
	}
	if !strings.Contains(raw, `"sections"`) || !strings.Contains(raw, `"Preferences"`) {
		t.Fatalf("profile is not grouped: %s", raw)
	}

	raw, err = callTool(t, session, "memory_stats", nil)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(raw, `"total_facts": 1`) {
		t.Fatalf("unexpected stats: %s", raw)
	}

	raw, err = callTool(t, session, "memory_forget", map[string]any{"id": remembered.ID})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !strings.Contains(raw, `"forgotten": true`) {
		t.Fatalf("unexpected forget result: %s", raw)
	}

	raw, err = callTool(t, session, "memory_search", map[string]any{"query": "favorite color"})
	if err != nil {
		t.Fatalf("search after forget: %v", err)
	}
	if strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("forgotten fact still recalled: %s", raw)
	}
}

func TestReadOnlyBlocksWrites(t *testing.T) {
	session := newSession(t, true)
	raw, err := callTool(t, session, "memory_remember", map[string]any{"content": "I live in Shanghai"})
	if err == nil {
		t.Fatalf("expected read-only error, got %s", raw)
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEntityScoping(t *testing.T) {
	session := newSession(t, false)
	if _, err := callTool(t, session, "memory_remember", map[string]any{
		"entity_id": "alice",
		"content":   "My favorite color is blue",
	}); err != nil {
		t.Fatalf("remember: %v", err)
	}

	raw, err := callTool(t, session, "memory_search", map[string]any{
		"entity_id": "bob",
		"query":     "favorite color",
	})
	if err != nil {
		t.Fatalf("bob search: %v", err)
	}
	if strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("bob saw alice's fact: %s", raw)
	}

	raw, err = callTool(t, session, "memory_search", map[string]any{
		"entity_id": "alice",
		"query":     "favorite color",
	})
	if err != nil {
		t.Fatalf("alice search: %v", err)
	}
	if !strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("alice lost her fact: %s", raw)
	}
}

func TestContextProfileControlsAndAbstention(t *testing.T) {
	session := newSession(t, false)
	if _, err := callTool(t, session, "memory_remember", map[string]any{
		"content": "My name is Alice",
	}); err != nil {
		t.Fatalf("remember: %v", err)
	}

	raw, err := callTool(t, session, "memory_context", map[string]any{
		"query":           "quantum flux capacitor",
		"disable_profile": true,
		"min_coverage":    0.9,
	})
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	var result struct {
		Text                 string  `json:"text"`
		InsufficientEvidence bool    `json:"insufficient_evidence"`
		Coverage             float64 `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode context result: %v", err)
	}
	if !result.InsufficientEvidence {
		t.Fatalf("expected abstention, coverage=%v: %s", result.Coverage, raw)
	}
	if strings.Contains(result.Text, "<memory_profile>") {
		t.Fatalf("disable_profile did not disable the profile: %s", raw)
	}
}

func TestListsMemoryTools(t *testing.T) {
	session := newSession(t, false)
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{
		"memory_search", "memory_context", "memory_remember",
		"memory_forget", "memory_profile", "memory_stats",
	} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}
