package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestEncodeDecodeEmbeddingRoundTrip(t *testing.T) {
	want := []float32{0, 1, -1, 0.5, 3.14159}
	encoded := EncodeEmbedding(want)
	got := DecodeEmbedding(encoded)
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value %d = %v, want %v", i, got[i], want[i])
		}
	}

	// Encoding is little-endian float32 regardless of host byte order.
	if len(encoded) != 4*len(want) {
		t.Fatalf("encoded length = %d", len(encoded))
	}
	if bits := binary.LittleEndian.Uint32(encoded[:4]); math.Float32frombits(bits) != 0 {
		t.Fatalf("first value = %v", math.Float32frombits(bits))
	}
}

func TestDecodeEmbeddingRejectsMalformedInput(t *testing.T) {
	if got := DecodeEmbedding(nil); got != nil {
		t.Fatalf("nil decode = %#v", got)
	}
	if got := DecodeEmbedding([]byte{1, 2, 3}); got != nil {
		t.Fatalf("short decode = %#v", got)
	}
}

func TestSQLFactUpsertPreserveInactive(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:upsert_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatal(err)
	}
	repos, ok := mgr.Driver().(Repos)
	if !ok {
		t.Fatal("driver does not implement Repos")
	}

	ctx := context.Background()
	entityID, err := repos.Entity().Create(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	in := FactInput{
		Content:        "I live in Shanghai",
		Embedding:      EncodeEmbedding([]float32{1}),
		Uniq:           "loc",
		EmbeddingModel: "test",
		EmbeddingDim:   1,
	}
	factID, err := repos.EntityFact().Upsert(ctx, entityID, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.EntityFact().Forget(ctx, entityID, factID); err != nil {
		t.Fatal(err)
	}

	in.PreserveInactive = true
	if _, err := repos.EntityFact().Upsert(ctx, entityID, in); err != nil {
		t.Fatal(err)
	}
	facts, err := repos.EntityFact().List(ctx, entityID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("preserve-inactive resurrected a deleted fact: %#v", facts)
	}

	in.PreserveInactive = false
	if _, err := repos.EntityFact().Upsert(ctx, entityID, in); err != nil {
		t.Fatal(err)
	}
	facts, err = repos.EntityFact().List(ctx, entityID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("explicit reactivation failed: %#v", facts)
	}
}

func TestDecodeAnyTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	if got, ok := decodeAnyTime(now); !ok || !got.Equal(now) {
		t.Fatalf("time.Time decode = %v, %v", got, ok)
	}
	if got, ok := decodeAnyTime(now.Format(time.RFC3339Nano)); !ok || !got.Equal(now) {
		t.Fatalf("RFC3339 decode = %v, %v", got, ok)
	}
	if got, ok := decodeAnyTime([]byte(now.Format("2006-01-02 15:04:05"))); !ok {
		t.Fatalf("byte time decode failed: %v", got)
	}
	if _, ok := decodeAnyTime("not a time"); ok {
		t.Fatal("invalid string accepted")
	}
	if _, ok := decodeAnyTime(42); ok {
		t.Fatal("invalid type accepted")
	}
}

func TestSQLPlaceholder(t *testing.T) {
	if got := sqlPlaceholder("postgres", 3); got != "$3" {
		t.Fatalf("postgres placeholder = %q", got)
	}
	if got := sqlPlaceholder("sqlite", 3); got != "?" {
		t.Fatalf("sqlite placeholder = %q", got)
	}
}

func TestNullableInt64(t *testing.T) {
	if got := nullableInt64(nil); got != nil {
		t.Fatalf("nil = %#v", got)
	}
	v := int64(7)
	if got := nullableInt64(&v); got != int64(7) {
		t.Fatalf("value = %#v", got)
	}
}

func TestEncodeEmbeddingEmpty(t *testing.T) {
	if got := EncodeEmbedding(nil); got != nil {
		t.Fatalf("empty encode = %#v", got)
	}
	if !bytes.Equal(EncodeEmbedding(nil), nil) {
		t.Fatal("empty encoding should be nil")
	}
}
