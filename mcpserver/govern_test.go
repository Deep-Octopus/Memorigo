package mcpserver_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Deep-Octopus/Memorigo/mcpserver"
)

func TestParseScopes(t *testing.T) {
	names, err := mcpserver.ParseScopes(" read, remember ")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names, ",")
	want := "memory_search,memory_context,memory_profile,memory_stats,memory_remember"
	if got != want {
		t.Fatalf("scopes = %s", got)
	}
	write, err := mcpserver.ParseScopes("write")
	if err != nil || !strings.Contains(strings.Join(write, ","), "memory_rule") {
		t.Fatalf("write scopes = %v, %v", write, err)
	}
	if _, err := mcpserver.ParseScopes("drop"); err == nil {
		t.Fatal("expected unknown scope error")
	}
	empty, err := mcpserver.ParseScopes("  ")
	if err != nil || empty != nil {
		t.Fatalf("blank scopes = %v, %v", empty, err)
	}
}

func TestScopeHidesWriteTools(t *testing.T) {
	session := newSessionWith(t, mcpserver.Options{Scopes: []string{"read"}})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range listed.Tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{"memory_search", "memory_context", "memory_profile", "memory_stats"} {
		if !seen[name] {
			t.Fatalf("missing %s in %v", name, seen)
		}
	}
	if seen["memory_remember"] || seen["memory_forget"] || seen["memory_rule"] {
		t.Fatalf("write tools were advertised: %v", seen)
	}
	if _, err := callTool(t, session, "memory_stats", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaAndAuditOmitMemoryText(t *testing.T) {
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	var audit bytes.Buffer
	session := newSessionWith(t, mcpserver.Options{
		QuotaPerMinute: 1,
		Audit:          &audit,
		Now:            func() time.Time { return now },
	})
	secret := "I like jazz records"
	if _, err := callTool(t, session, "memory_remember", map[string]any{
		"entity_id": "alice\ninjected",
		"content":   secret,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(t, session, "memory_search", map[string]any{
		"entity_id": "alice\ninjected",
		"query":     secret,
	}); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("expected quota error, got %v", err)
	}
	now = now.Add(61 * time.Second)
	raw, err := callTool(t, session, "memory_search", map[string]any{
		"entity_id": "alice\ninjected",
		"query":     "jazz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "jazz") {
		t.Fatalf("recall missed fact: %s", raw)
	}
	logText := audit.String()
	if strings.Contains(logText, "jazz") {
		t.Fatalf("audit leaked memory text: %s", logText)
	}
	lines := strings.Split(strings.TrimSpace(logText), "\n")
	if len(lines) != 3 {
		t.Fatalf("audit lines = %d\n%s", len(lines), logText)
	}
	if !strings.Contains(lines[0], `"entity_id":"alice injected"`) || !strings.Contains(lines[0], `"outcome":"ok"`) {
		t.Fatalf("first audit line = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"outcome":"quota"`) {
		t.Fatalf("quota line = %s", lines[1])
	}
}

func TestReadOnlyAuditDoesNotStore(t *testing.T) {
	var audit bytes.Buffer
	session := newSessionWith(t, mcpserver.Options{ReadOnly: true, Audit: &audit})
	_, err := callTool(t, session, "memory_remember", map[string]any{"content": "I live in Shanghai"})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only, got %v", err)
	}
	if !strings.Contains(audit.String(), `"outcome":"readonly"`) {
		t.Fatalf("audit = %s", audit.String())
	}
	if strings.Contains(audit.String(), "Shanghai") {
		t.Fatalf("audit leaked content: %s", audit.String())
	}
}

func TestAuditWriteFailureStillRemembers(t *testing.T) {
	session := newSessionWith(t, mcpserver.Options{Audit: failWriter{}})
	raw, err := callTool(t, session, "memory_remember", map[string]any{"content": "I like jazz records"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"id"`) {
		t.Fatalf("remember result: %s", raw)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errAudit }

var errAudit = errString("disk full")

type errString string

func (e errString) Error() string { return string(e) }

func newSessionWith(t *testing.T, opts mcpserver.Options) *mcp.ClientSession {
	t.Helper()
	server := mcpserver.New(newMemori(t), opts)
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
