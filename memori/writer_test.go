package memori

import (
	"testing"

	"github.com/Deep-Octopus/Memorigo/storage"
)

func TestSplitNewMessagesFullHistory(t *testing.T) {
	stored := []storage.StoredMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	incoming := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "how are you"},
	}
	got := splitNewMessages(stored, incoming)
	if len(got) != 1 || got[0].Content != "how are you" {
		t.Fatalf("got %#v", got)
	}
}

func TestSplitNewMessagesOnlyNew(t *testing.T) {
	stored := []storage.StoredMessage{{Role: "user", Content: "hello"}}
	incoming := []Message{{Role: "user", Content: "brand new"}}
	got := splitNewMessages(stored, incoming)
	if len(got) != 1 || got[0].Content != "brand new" {
		t.Fatalf("got %#v", got)
	}
}

func TestSplitNewMessagesTrimmedHistory(t *testing.T) {
	stored := []storage.StoredMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
	}
	incoming := []Message{
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	got := splitNewMessages(stored, incoming)
	// Incoming's prefix "u2" matches stored's suffix; "a2" is new.
	if len(got) != 1 || got[0].Content != "a2" {
		t.Fatalf("got %#v", got)
	}
}

func TestSplitNewMessagesNoOverlap(t *testing.T) {
	stored := []storage.StoredMessage{{Role: "user", Content: "old"}}
	incoming := []Message{
		{Role: "user", Content: "one"},
		{Role: "user", Content: "two"},
	}
	got := splitNewMessages(stored, incoming)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
}

func TestSplitNewMessagesEmptyStored(t *testing.T) {
	incoming := []Message{{Role: "user", Content: "first"}}
	got := splitNewMessages(nil, incoming)
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestNonSystemMessages(t *testing.T) {
	got := nonSystemMessages([]Message{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "   "},
		{Role: "assistant", Content: "hi"},
	})
	if len(got) != 2 || got[0].Content != "hello" || got[1].Content != "hi" {
		t.Fatalf("got %#v", got)
	}
}

func TestMergeSummary(t *testing.T) {
	if got := mergeSummary("", "one"); got != "one" {
		t.Fatalf("got %q", got)
	}
	if got := mergeSummary("one", ""); got != "one" {
		t.Fatalf("got %q", got)
	}
	if got := mergeSummary("one two", "two"); got != "one two" {
		t.Fatalf("got %q", got)
	}
	if got := mergeSummary("one", "one two three"); got != "one two three" {
		t.Fatalf("got %q", got)
	}
	if got := mergeSummary("one", "two"); got != "one；two" {
		t.Fatalf("got %q", got)
	}
}

func TestContentHashStable(t *testing.T) {
	a := contentHash("My favorite color is blue.")
	b := contentHash("my favorite color: blue")
	if a != b {
		t.Fatalf("hash mismatch: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex length = %d", len(a))
	}
}

func TestIsRetriableError(t *testing.T) {
	if !isRetriableError(errString("restart transaction")) {
		t.Fatal("restart transaction should retry")
	}
	if !isRetriableError(errString("ERROR: deadlock detected")) {
		t.Fatal("deadlock should retry")
	}
	if isRetriableError(errString("syntax error")) {
		t.Fatal("syntax error should not retry")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
