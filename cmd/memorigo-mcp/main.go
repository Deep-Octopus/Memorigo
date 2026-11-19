// Command memorigo-mcp serves Memorigo memory tools over the Model Context
// Protocol. It supports stdio (local clients) and stateless Streamable HTTP
// (remote clients). Logs go to stderr; stdio mode reserves stdout for MCP.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/internal/version"
	"github.com/Deep-Octopus/Memorigo/mcpserver"
	"github.com/Deep-Octopus/Memorigo/memori"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dbPath := flag.String("db", env("MEMORI_DB", "memorigo.db"), "SQLite database path")
	entityID := flag.String("entity", env("MEMORI_ENTITY_ID", "mcp-user"), "default entity id when a tool call omits entity_id")
	processID := flag.String("process", env("MEMORI_PROCESS_ID", "memorigo-mcp"), "process id recorded in provenance")
	readOnly := flag.Bool("readonly", envBool("MEMORI_MCP_READONLY", false), "disable memory_remember and memory_forget")
	scopesRaw := flag.String("scopes", env("MEMORI_MCP_SCOPES", ""), "comma-separated tool allow-list (empty allows every tool; aliases: read, write, all)")
	quota := flag.Int("quota", envInt("MEMORI_MCP_QUOTA", 0), "max tool calls per minute in this process (0 is unlimited)")
	auditPath := flag.String("audit", env("MEMORI_MCP_AUDIT_LOG", ""), "append a JSONL audit log; empty disables it")
	httpMode := flag.Bool("http", envBool("MEMORI_MCP_HTTP", false), "serve stateless Streamable HTTP instead of stdio")
	addr := flag.String("addr", env("MEMORI_MCP_ADDR", "127.0.0.1:8787"), "HTTP listen address")
	token := flag.String("token", env("MEMORI_MCP_TOKEN", ""), "HTTP Bearer token (or MEMORI_MCP_TOKEN)")
	oauthIssuer := flag.String("oauth-issuer", env("MEMORI_MCP_OAUTH_ISSUER", ""), "OAuth issuer URL checked as JWT iss. Empty disables JWT tokens")
	oauthAudience := flag.String("oauth-audience", env("MEMORI_MCP_OAUTH_AUDIENCE", ""), "OAuth audience URL checked as JWT aud. Also the protected-resource identifier")
	oauthHMAC := flag.String("oauth-hmac-secret", env("MEMORI_MCP_OAUTH_HMAC_SECRET", ""), "HS256 secret, at least 16 bytes. Mutually exclusive with -oauth-rsa-key")
	oauthRSAPath := flag.String("oauth-rsa-key", env("MEMORI_MCP_OAUTH_RSA_KEY", ""), "PEM file containing an RS256 public key")
	allowAnonymous := flag.Bool("allow-anonymous", envBool("MEMORI_MCP_ALLOW_ANONYMOUS", false), "allow unauthenticated HTTP (trusted loopback only)")
	path := flag.String("path", env("MEMORI_MCP_PATH", "/mcp"), "HTTP MCP endpoint path")
	tlsCert := flag.String("tls-cert", env("MEMORI_MCP_TLS_CERT", ""), "TLS certificate file for HTTP mode")
	tlsKey := flag.String("tls-key", env("MEMORI_MCP_TLS_KEY", ""), "TLS private key file for HTTP mode")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return nil
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("-tls-cert and -tls-key must be provided together")
	}
	scopes, err := mcpserver.ParseScopes(*scopesRaw)
	if err != nil {
		return err
	}
	if *quota < 0 || *quota > mcpserver.MaxQuotaPerMinute {
		return fmt.Errorf("quota must be between 0 and %d", mcpserver.MaxQuotaPerMinute)
	}
	rsaPEM, err := readOptionalFile(*oauthRSAPath)
	if err != nil {
		return err
	}
	oauthCfg := mcpserver.OAuthConfig{
		Issuer:          *oauthIssuer,
		Audience:        *oauthAudience,
		HMACSecret:      *oauthHMAC,
		RSAPublicKeyPEM: rsaPEM,
	}
	if !*httpMode && oauthConfigured(oauthCfg) {
		return errors.New("OAuth flags apply only to -http")
	}
	audit, auditCloser, err := openAudit(*auditPath)
	if err != nil {
		return err
	}
	if auditCloser != nil {
		defer auditCloser.Close()
	}

	db, err := openSQLite(*dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	m := memori.New(memori.WithStorageConn(db))
	defer m.Close()
	if err := m.Err(); err != nil {
		return fmt.Errorf("initialize memorigo: %w", err)
	}

	server := mcpserver.New(m, mcpserver.Options{
		EntityID:       *entityID,
		ProcessID:      *processID,
		ReadOnly:       *readOnly,
		ServerVersion:  version.Version,
		Scopes:         scopes,
		QuotaPerMinute: *quota,
		Audit:          audit,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("memorigo MCP governance readonly=%v scopes=%s quota=%d audit=%v",
		*readOnly, scopeLabel(scopes), *quota, audit != nil)
	if *httpMode {
		return serveHTTP(ctx, server, httpConfig{
			addr:           *addr,
			path:           *path,
			token:          *token,
			allowAnonymous: *allowAnonymous,
			tlsCert:        *tlsCert,
			tlsKey:         *tlsKey,
			oauth:          oauthCfg,
		})
	}

	log.Printf("memorigo MCP server ready transport=stdio db=%s entity=%s readonly=%v scopes=%s quota=%d audit=%v version=%s",
		*dbPath, *entityID, *readOnly, scopeLabel(scopes), *quota, audit != nil, version.Version)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp server: %w", err)
	}
	if err := m.Flush(context.Background()); err != nil {
		log.Printf("flush on shutdown: %v", err)
	}
	return nil
}

type httpConfig struct {
	addr           string
	path           string
	token          string
	allowAnonymous bool
	tlsCert        string
	tlsKey         string
	oauth          mcpserver.OAuthConfig
}

func serveHTTP(ctx context.Context, server *mcp.Server, cfg httpConfig) error {
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{
		Token:          cfg.token,
		AllowAnonymous: cfg.allowAnonymous,
		Path:           cfg.path,
		Version:        version.Version,
		OAuth:          cfg.oauth,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	useTLS := cfg.tlsCert != ""
	if !useTLS && !isLoopbackAddr(cfg.addr) {
		log.Printf("warning: HTTP transport is not using TLS; terminate TLS at a reverse proxy")
	}
	if cfg.token == "" {
		log.Printf("warning: HTTP transport is unauthenticated (allow-anonymous)")
	}
	log.Printf("memorigo MCP server ready transport=http addr=%s path=%s tls=%v oauth=%v version=%s",
		cfg.addr, cfg.path, useTLS, oauthConfigured(cfg.oauth), version.Version)

	errCh := make(chan error, 1)
	go func() {
		if useTLS {
			errCh <- srv.ListenAndServeTLS(cfg.tlsCert, cfg.tlsKey)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	}
	return nil
}

func oauthConfigured(cfg mcpserver.OAuthConfig) bool {
	return strings.TrimSpace(cfg.Issuer) != "" || strings.TrimSpace(cfg.Audience) != "" ||
		cfg.HMACSecret != "" || strings.TrimSpace(cfg.RSAPublicKeyPEM) != ""
}

func readOptionalFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func isLoopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func openSQLite(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("empty database path")
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:memorigo_mcp?mode=memory&cache=shared"
	} else if !strings.HasPrefix(path, "file:") {
		dsn = "file:" + path
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func openAudit(path string) (io.Writer, io.Closer, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open audit log: %w", err)
	}
	return f, f, nil
}

func scopeLabel(scopes []string) string {
	if len(scopes) == 0 {
		return "all"
	}
	return strings.Join(scopes, ",")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}
