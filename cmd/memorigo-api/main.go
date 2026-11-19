// Command memorigo-api serves the JSON HTTP API in package api.
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

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/api"
	"github.com/Deep-Octopus/Memorigo/internal/version"
	"github.com/Deep-Octopus/Memorigo/memori"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dbPath := flag.String("db", env("MEMORI_DB", "memorigo.db"), "SQLite database path")
	entityID := flag.String("entity", env("MEMORI_ENTITY_ID", "api-user"), "default entity id")
	processID := flag.String("process", env("MEMORI_PROCESS_ID", "memorigo-api"), "process id recorded on writes")
	readOnly := flag.Bool("readonly", envBool("MEMORI_API_READONLY", false), "reject remember, forget, restore, maintain and delete")
	addr := flag.String("addr", env("MEMORI_API_ADDR", "127.0.0.1:8788"), "listen address")
	token := flag.String("token", env("MEMORI_API_TOKEN", ""), "Bearer token")
	allowAnonymous := flag.Bool("allow-anonymous", envBool("MEMORI_API_ALLOW_ANONYMOUS", false), "allow requests without a token")
	tlsCert := flag.String("tls-cert", env("MEMORI_API_TLS_CERT", ""), "TLS certificate file")
	tlsKey := flag.String("tls-key", env("MEMORI_API_TLS_KEY", ""), "TLS private key file")
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

	handler, err := api.NewHandler(m, api.Options{
		EntityID:       *entityID,
		ProcessID:      *processID,
		ReadOnly:       *readOnly,
		Token:          *token,
		AllowAnonymous: *allowAnonymous,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if *tlsCert == "" && !isLoopbackAddr(*addr) {
		log.Printf("warning: API is not using TLS; terminate TLS at a reverse proxy")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("memorigo API ready addr=%s tls=%v readonly=%v version=%s", *addr, *tlsCert != "", *readOnly, version.Version)
		if *tlsCert != "" {
			errCh <- srv.ListenAndServeTLS(*tlsCert, *tlsKey)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	if err := m.Flush(context.Background()); err != nil {
		log.Printf("flush on shutdown: %v", err)
	}
	return nil
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
		dsn = "file:memorigo_api?mode=memory&cache=shared"
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

func isLoopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
