package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteMemory(t *testing.T) {
	db, err := openSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "memory.db")
	db, err := openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("MEMORI_MCP_QUOTA", "")
	if envInt("MEMORI_MCP_QUOTA", 0) != 0 {
		t.Fatal("empty env should use fallback")
	}
	t.Setenv("MEMORI_MCP_QUOTA", "40")
	if envInt("MEMORI_MCP_QUOTA", 0) != 40 {
		t.Fatal("expected 40")
	}
	t.Setenv("MEMORI_MCP_QUOTA", "nope")
	if envInt("MEMORI_MCP_QUOTA", 0) != -1 {
		t.Fatal("invalid env should be rejected by the caller")
	}
}

func TestOpenAuditFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, closer, err := openAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("{\"tool\":\"memory_stats\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode = %o", info.Mode().Perm())
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		if !isLoopbackAddr(addr) {
			t.Errorf("%q should be loopback", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8787", ":8787", "10.0.0.1:8787"} {
		if isLoopbackAddr(addr) {
			t.Errorf("%q should not be loopback", addr)
		}
	}
}
