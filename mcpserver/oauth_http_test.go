package mcpserver_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Deep-Octopus/Memorigo/mcpserver"
)

func TestOAuthMetadataStaticAndJWT(t *testing.T) {
	const (
		secret   = "0123456789abcdef"
		audience = "http://memory.example/mcp"
		issuer   = "http://issuer.example"
	)
	var audit bytes.Buffer
	server := mcpserver.New(newMemori(t), mcpserver.Options{
		ServerVersion:  "test",
		QuotaPerMinute: 2,
		Audit:          &audit,
	})
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{
		Token:   "secret",
		Version: "test",
		OAuth: mcpserver.OAuthConfig{
			Issuer:     issuer,
			Audience:   audience,
			HMACSecret: secret,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"resource":"http://memory.example/mcp"`) {
		t.Fatalf("metadata = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"authorization_servers":["http://issuer.example"]`) {
		t.Fatalf("metadata missing issuer: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `resource_metadata="http://memory.example/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
	if audit.Len() != 0 {
		t.Fatalf("auth failure wrote an audit line: %s", audit.String())
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()
	readToken := signTestJWT(t, secret, map[string]any{
		"iss":   issuer,
		"aud":   audience,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"scope": "read",
	})
	session := connectHTTP(t, ts.URL+"/mcp", readToken)
	_, err = callTool(t, session, "memory_remember", map[string]any{"content": "secret-color-blue"})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("read token remember = %v", err)
	}
	if _, err := callTool(t, session, "memory_search", map[string]any{"query": "color"}); err != nil {
		t.Fatalf("read token search: %v", err)
	}
	if strings.Contains(audit.String(), "secret-color-blue") {
		t.Fatalf("audit contains memory text: %s", audit.String())
	}
	if !strings.Contains(audit.String(), `"outcome":"forbidden"`) {
		t.Fatalf("audit = %s", audit.String())
	}

	static := connectHTTP(t, ts.URL+"/mcp", "secret")
	if _, err := callTool(t, static, "memory_remember", map[string]any{"content": "static bearer fact"}); err != nil {
		t.Fatalf("static bearer remember: %v", err)
	}
	if _, err := callTool(t, static, "memory_remember", map[string]any{"content": "second"}); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("second remember = %v, want quota", err)
	}
}

func TestOAuthTokenCannotWidenProcessScopes(t *testing.T) {
	const secret = "0123456789abcdef"
	scopes, err := mcpserver.ParseScopes("read")
	if err != nil {
		t.Fatal(err)
	}
	server := mcpserver.New(newMemori(t), mcpserver.Options{ServerVersion: "test", Scopes: scopes})
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{
		OAuth: mcpserver.OAuthConfig{
			Issuer:     "http://issuer.example",
			Audience:   "http://memory.example/mcp",
			HMACSecret: secret,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	token := signTestJWT(t, secret, map[string]any{
		"iss":   "http://issuer.example",
		"aud":   "http://memory.example/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"scope": "all",
	})
	session := connectHTTP(t, ts.URL+"/mcp", token)
	if _, err := callTool(t, session, "memory_search", map[string]any{"query": "color"}); err != nil {
		t.Fatalf("search inside process scope: %v", err)
	}
	if _, err := callTool(t, session, "memory_remember", map[string]any{"content": "nope"}); err == nil {
		t.Fatal("token widened the process allow-list")
	}
}

func connectHTTP(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "memorigo-oauth-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: authTransport{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func signTestJWT(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
