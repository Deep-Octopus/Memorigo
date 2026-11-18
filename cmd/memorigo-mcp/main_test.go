package main

import (
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
