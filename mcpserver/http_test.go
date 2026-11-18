package mcpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Deep-Octopus/Memorigo/mcpserver"
)

type authTransport struct {
	token string
}

func (a authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func TestHTTPHandlerRequiresToken(t *testing.T) {
	server := mcpserver.New(newMemori(t), mcpserver.Options{ServerVersion: "test"})
	if _, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{}); err == nil {
		t.Fatal("expected error without token")
	}
	for _, path := range []string{"mcp", "/", "/healthz", "/mcp path"} {
		if _, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{AllowAnonymous: true, Path: path}); err == nil {
			t.Errorf("expected error for path %q", path)
		}
	}
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{AllowAnonymous: true})
	if err != nil {
		t.Fatalf("anonymous handler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("anonymous handler rejected request")
	}
}

func TestHTTPHealthAndAuth(t *testing.T) {
	server := mcpserver.New(newMemori(t), mcpserver.Options{ServerVersion: "test"})
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{Token: "secret", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"service":"memorigo"`) {
		t.Fatalf("healthz = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate header")
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "bearer secret")
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("scheme should be case-insensitive")
	}
}

func TestHTTPProxyHostAndCrossOrigin(t *testing.T) {
	server := mcpserver.New(newMemori(t), mcpserver.Options{ServerVersion: "test"})
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{Token: "secret", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Host = "memory.example.com"
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		return req
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newReq())
	if rec.Code == http.StatusForbidden {
		t.Fatalf("reverse-proxy Host was rejected: %d %s", rec.Code, rec.Body.String())
	}

	req := newReq()
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request status = %d, want 403", rec.Code)
	}
}

func TestHTTPStreamableTools(t *testing.T) {
	server := mcpserver.New(newMemori(t), mcpserver.Options{ServerVersion: "test"})
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{Token: "secret", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "memorigo-http-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             ts.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: authTransport{token: "secret"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	raw, err := callTool(t, session, "memory_remember", map[string]any{
		"content": "My favorite color is blue",
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if !strings.Contains(raw, `"id"`) {
		t.Fatalf("unexpected remember result: %s", raw)
	}

	raw, err = callTool(t, session, "memory_search", map[string]any{"query": "favorite color"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(strings.ToLower(raw), "blue") {
		t.Fatalf("search missed fact: %s", raw)
	}
}
