package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/api"
	"github.com/Deep-Octopus/Memorigo/memori"
)

var dbCounter atomic.Int64

func newMemori(t *testing.T) *memori.Memori {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:api_test_%d?mode=memory&cache=shared", dbCounter.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	m := memori.New(memori.WithStorageConn(db), memori.WithConfig(func(c *memori.Config) {
		c.LogLevel = "off"
		c.Embedding.Provider = "hash"
		c.Extraction.Provider = "heuristic"
		c.Recall.MinScore = 0.01
	}))
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

func newServer(t *testing.T, opts api.Options) *httptest.Server {
	t.Helper()
	if opts.Token == "" && !opts.AllowAnonymous {
		opts.Token = "secret"
	}
	handler, err := api.NewHandler(newMemori(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func TestHealthAndAuth(t *testing.T) {
	srv := newServer(t, api.Options{Token: "secret"})
	status, body := do(t, srv, http.MethodGet, "/healthz", "", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte("ok")) {
		t.Fatalf("health = %d %s", status, body)
	}
	status, _ = do(t, srv, http.MethodPost, "/v1/recall", "", map[string]string{"query": "jazz"})
	if status != http.StatusUnauthorized {
		t.Fatalf("missing token = %d", status)
	}
	status, _ = do(t, srv, http.MethodPost, "/v1/recall", "nope", map[string]string{"query": "jazz"})
	if status != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", status)
	}
	if _, err := api.NewHandler(newMemori(t), api.Options{}); err == nil {
		t.Fatal("anonymous without a flag was accepted")
	}
}

func TestRememberRecallExportDelete(t *testing.T) {
	srv := newServer(t, api.Options{Token: "secret", EntityID: "default"})
	status, body := do(t, srv, http.MethodPost, "/v1/remember", "secret", map[string]string{
		"entity_id": "alice",
		"content":   "I like jazz records",
	})
	if status != http.StatusOK {
		t.Fatalf("remember = %d %s", status, body)
	}
	var remembered struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &remembered); err != nil || remembered.ID == 0 {
		t.Fatalf("id = %s (%v)", body, err)
	}

	status, body = do(t, srv, http.MethodPost, "/v1/recall", "secret", map[string]any{
		"entity_id": "bob",
		"query":     "jazz",
		"limit":     5,
	})
	if status != http.StatusOK || bytes.Contains(body, []byte("jazz")) {
		t.Fatalf("bob recall = %d %s", status, body)
	}

	status, body = do(t, srv, http.MethodPost, "/v1/recall", "secret", map[string]any{
		"entity_id": "alice",
		"query":     "jazz records",
		"limit":     5,
	})
	if status != http.StatusOK || !bytes.Contains(body, []byte("jazz")) {
		t.Fatalf("alice recall = %d %s", status, body)
	}

	status, body = do(t, srv, http.MethodGet, "/v1/export?entity_id=alice", "secret", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"version":1`)) || !bytes.Contains(body, []byte("jazz")) {
		t.Fatalf("export = %d %s", status, body)
	}

	status, body = do(t, srv, http.MethodPost, "/v1/context", "secret", map[string]any{
		"entity_id": "alice",
		"query":     "jazz",
		"budget":    400,
	})
	if status != http.StatusOK || !bytes.Contains(body, []byte("jazz")) {
		t.Fatalf("context = %d %s", status, body)
	}

	status, body = do(t, srv, http.MethodDelete, "/v1/subject?entity_id=alice", "secret", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"entities":1`)) {
		t.Fatalf("delete = %d %s", status, body)
	}
	status, body = do(t, srv, http.MethodPost, "/v1/recall", "secret", map[string]any{
		"entity_id": "alice",
		"query":     "jazz",
	})
	if status != http.StatusOK || bytes.Contains(body, []byte("jazz")) {
		t.Fatalf("after delete = %d %s", status, body)
	}
}

func TestRuleAppearsInProfile(t *testing.T) {
	srv := newServer(t, api.Options{Token: "secret"})
	status, body := do(t, srv, http.MethodPost, "/v1/rules", "secret", map[string]string{
		"entity_id": "alice",
		"content":   "Always answer in Go",
	})
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"id"`)) {
		t.Fatalf("rule = %d %s", status, body)
	}
	status, body = do(t, srv, http.MethodGet, "/v1/profile?entity_id=alice", "secret", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte("Always answer in Go")) || !bytes.Contains(body, []byte("Rules")) {
		t.Fatalf("profile = %d %s", status, body)
	}
}

func TestReadOnlyBlocksWrites(t *testing.T) {
	srv := newServer(t, api.Options{Token: "secret", ReadOnly: true})
	status, _ := do(t, srv, http.MethodPost, "/v1/remember", "secret", map[string]string{"content": "I like jazz records"})
	if status != http.StatusForbidden {
		t.Fatalf("remember status = %d", status)
	}
	status, _ = do(t, srv, http.MethodPost, "/v1/rules", "secret", map[string]string{"content": "Always answer in Go"})
	if status != http.StatusForbidden {
		t.Fatalf("rule status = %d", status)
	}
	status, body := do(t, srv, http.MethodPost, "/v1/maintain", "secret", map[string]any{
		"ttl":     "1h",
		"dry_run": true,
	})
	if status != http.StatusOK {
		t.Fatalf("dry run = %d %s", status, body)
	}
}
