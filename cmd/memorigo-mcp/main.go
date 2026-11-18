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
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	httpMode := flag.Bool("http", envBool("MEMORI_MCP_HTTP", false), "serve stateless Streamable HTTP instead of stdio")
	addr := flag.String("addr", env("MEMORI_MCP_ADDR", "127.0.0.1:8787"), "HTTP listen address")
	token := flag.String("token", env("MEMORI_MCP_TOKEN", ""), "HTTP Bearer token (or MEMORI_MCP_TOKEN)")
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
		EntityID:      *entityID,
		ProcessID:     *processID,
		ReadOnly:      *readOnly,
		ServerVersion: version.Version,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpMode {
		return serveHTTP(ctx, server, httpConfig{
			addr:           *addr,
			path:           *path,
			token:          *token,
			allowAnonymous: *allowAnonymous,
			tlsCert:        *tlsCert,
			tlsKey:         *tlsKey,
		})
	}

	log.Printf("memorigo MCP server ready transport=stdio db=%s entity=%s readonly=%v version=%s",
		*dbPath, *entityID, *readOnly, version.Version)
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
}

func serveHTTP(ctx context.Context, server *mcp.Server, cfg httpConfig) error {
	handler, err := mcpserver.NewHTTPHandler(server, mcpserver.HTTPOptions{
		Token:          cfg.token,
		AllowAnonymous: cfg.allowAnonymous,
		Path:           cfg.path,
		Version:        version.Version,
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
	log.Printf("memorigo MCP server ready transport=http addr=%s path=%s tls=%v version=%s",
		cfg.addr, cfg.path, useTLS, version.Version)

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
