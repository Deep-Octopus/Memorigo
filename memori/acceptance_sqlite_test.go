package memori_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestAcceptance_WriteAugmentRecall(t *testing.T) {
	m := newTestMemori(t)
	if err := m.TryAttribution("user-123", "proc-abc"); err != nil {
		t.Fatal(err)
	}

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My favorite color is blue"}},
		&memori.Message{Role: "assistant", Content: "Got it."},
	)
	flushMemori(t, m)

	facts, err := m.Recall("favorite color", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(facts) == 0 {
		t.Fatal("expected recalled facts")
	}
	if facts[0].Content != "My favorite color is blue" {
		t.Fatalf("top fact = %q", facts[0].Content)
	}
	if facts[0].Score <= 0 || facts[0].Similarity <= 0 {
		t.Fatalf("score components missing: %#v", facts[0])
	}
}

func TestAcceptance_RecallWithoutAttributionReturnsEmpty(t *testing.T) {
	m := newTestMemori(t)
	facts, err := m.Recall("anything", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected no facts, got %#v", facts)
	}
}

func TestRememberSupersedesConflictingSlot(t *testing.T) {
	m := newTestMemori(t)
	if err := m.TryAttribution("user-1", "proc"); err != nil {
		t.Fatal(err)
	}

	blueID, err := m.Remember("My favorite color is blue")
	if err != nil {
		t.Fatal(err)
	}
	redID, err := m.Remember("My favorite color is red")
	if err != nil {
		t.Fatal(err)
	}
	if blueID == redID {
		t.Fatal("expected a new fact row for the new value")
	}

	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Content != "My favorite color is red" {
		t.Fatalf("active facts = %#v", facts)
	}

	recalled, err := m.Recall("favorite color", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled) == 0 || !strings.Contains(recalled[0].Content, "red") {
		t.Fatalf("recall should return the superseding fact: %#v", recalled)
	}

	stats, err := m.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalFacts != 2 || stats.ActiveFacts != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestRememberForget(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user-1", "proc")

	id, err := m.Remember("I live in Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Forget(id); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("forgotten fact still visible: %#v", facts)
	}
	recalled, err := m.Recall("where do I live", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled) != 0 {
		t.Fatalf("forgotten fact recalled: %#v", recalled)
	}
}

func TestAttributionIsolation(t *testing.T) {
	m := newTestMemori(t)

	_ = m.TryAttribution("alice", "proc")
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}

	_ = m.TryAttribution("bob", "proc")
	if _, err := m.Remember("My favorite color is red"); err != nil {
		t.Fatal(err)
	}

	bobFacts, err := m.Recall("favorite color", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range bobFacts {
		if strings.Contains(f.Content, "blue") {
			t.Fatalf("alice's fact leaked to bob: %#v", bobFacts)
		}
	}

	_ = m.TryAttribution("alice", "proc")
	aliceFacts, err := m.Recall("favorite color", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceFacts) == 0 || !strings.Contains(aliceFacts[0].Content, "blue") {
		t.Fatalf("alice recall = %#v", aliceFacts)
	}
}

func TestWriterDeduplicatesFullHistory(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My name is Bob"}},
		&memori.Message{Role: "assistant", Content: "Nice to meet you"},
	)
	flushMemori(t, m)

	writeConversation(t, m,
		[]memori.Message{
			{Role: "user", Content: "My name is Bob"},
			{Role: "assistant", Content: "Nice to meet you"},
			{Role: "user", Content: "What is my name?"},
		},
		&memori.Message{Role: "assistant", Content: "Your name is Bob"},
	)
	flushMemori(t, m)

	msgs := recentStoredMessages(t, m)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 stored messages, got %d: %#v", len(msgs), msgs)
	}
	want := []string{"My name is Bob", "Nice to meet you", "What is my name?", "Your name is Bob"}
	for i, w := range want {
		if msgs[i].Content != w {
			t.Fatalf("message %d = %q, want %q", i, msgs[i].Content, w)
		}
	}
}

func TestWriterReplayDoesNotInflateFrequency(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	history := []memori.Message{
		{Role: "user", Content: "My favorite color is blue"},
		{Role: "assistant", Content: "Noted"},
	}
	writeConversation(t, m, history, &memori.Message{Role: "assistant", Content: "Noted"})
	flushMemori(t, m)

	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %#v", facts)
	}
	before := facts[0].NumTimes

	for i := 0; i < 3; i++ {
		writeConversation(t, m, history, &memori.Message{Role: "assistant", Content: "Noted"})
	}
	flushMemori(t, m)

	facts, err = m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].NumTimes != before {
		t.Fatalf("replay changed frequency: before=%d after=%#v", before, facts)
	}
	if msgs := recentStoredMessages(t, m); len(msgs) != 2 {
		t.Fatalf("replay wrote messages: %#v", msgs)
	}
}

func TestRememberDistinguishesSingleCharacterValues(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	seven, err := m.Remember("My favorite number is 7")
	if err != nil {
		t.Fatal(err)
	}
	eight, err := m.Remember("My favorite number is 8")
	if err != nil {
		t.Fatal(err)
	}
	if seven == eight {
		t.Fatal("different values collapsed into one fact")
	}
	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !strings.Contains(facts[0].Content, "8") {
		t.Fatalf("active facts = %#v", facts)
	}
}

func TestExtractionDoesNotResurrectForgottenFact(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	id, err := m.Remember("I live in Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Forget(id); err != nil {
		t.Fatal(err)
	}

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "I live in Shanghai"}},
		&memori.Message{Role: "assistant", Content: "Noted"},
	)
	flushMemori(t, m)

	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("forgotten fact resurrected by replay: %#v", facts)
	}
}

func TestExtractionDoesNotRevertSupersededSlot(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("My favorite color is red"); err != nil {
		t.Fatal(err)
	}

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My favorite color is blue"}},
		&memori.Message{Role: "assistant", Content: "Noted"},
	)
	flushMemori(t, m)

	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !strings.Contains(facts[0].Content, "red") {
		t.Fatalf("stale replay reverted slot: %#v", facts)
	}
}

func TestWriterDoesNotReextractPriorTurns(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My favorite color is blue"}},
		&memori.Message{Role: "assistant", Content: "Noted"},
	)
	flushMemori(t, m)

	writeConversation(t, m,
		[]memori.Message{
			{Role: "user", Content: "My favorite color is blue"},
			{Role: "assistant", Content: "Noted"},
			{Role: "user", Content: "What is my favorite color?"},
		},
		&memori.Message{Role: "assistant", Content: "It is blue."},
	)
	flushMemori(t, m)

	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %#v", facts)
	}
	if facts[0].NumTimes != 1 {
		t.Fatalf("prior turn re-extracted: num_times = %d", facts[0].NumTimes)
	}
}

func TestWriterRecoversAfterRolledBackIDs(t *testing.T) {
	m, db := newTestMemoriDB(t)
	_ = m.TryAttribution("user", "proc")

	if _, err := db.Exec(`CREATE TRIGGER fail_message BEFORE INSERT ON memori_conversation_message
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	err := memori.NewWriter(m).Execute(context.Background(), memori.ConversationPayload{
		Messages: []memori.Message{{Role: "user", Content: "hello"}},
		Response: &memori.Message{Role: "assistant", Content: "hi"},
	})
	if err == nil {
		t.Fatal("expected the trigger to abort the write")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_message`); err != nil {
		t.Fatal(err)
	}

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "I live in Shanghai"}},
		&memori.Message{Role: "assistant", Content: "Noted"},
	)
	flushMemori(t, m)

	facts, err := m.Recall("where do I live", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || !strings.Contains(facts[0].Content, "Shanghai") {
		t.Fatalf("writer did not recover from rolled-back ids: %#v", facts)
	}
}

func TestConcurrentSlotWritesLeaveOneActiveFact(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.Remember(fmt.Sprintf("My favorite color is shade-%d", i)); err != nil {
				t.Errorf("remember %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	facts, err := m.Facts(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("active facts = %d, want 1: %#v", len(facts), facts)
	}
}

func TestReindexHandlesMisreportedDimension(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}

	m.Embedder = &misreportedDimensionEmbedder{
		Embedder: embed.NewHashEmbedderWithDim(2),
		reported: 1536,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := m.Reindex(ctx)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if n != 1 {
		t.Fatalf("reindexed = %d, want 1", n)
	}
}

type misreportedDimensionEmbedder struct {
	embed.Embedder
	reported int
}

func (e *misreportedDimensionEmbedder) Dimension() int { return e.reported }

func TestNewSessionIsolatesConversations(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "first session"}},
		&memori.Message{Role: "assistant", Content: "one"},
	)
	flushMemori(t, m)
	firstSessionID := m.SessionID()
	firstMessages := recentStoredMessages(t, m)

	m.NewSession()
	if m.SessionID() == firstSessionID {
		t.Fatal("NewSession did not rotate the session id")
	}
	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "second session"}},
		&memori.Message{Role: "assistant", Content: "two"},
	)
	flushMemori(t, m)

	secondMessages := recentStoredMessages(t, m)
	if len(firstMessages) != 2 || len(secondMessages) != 2 {
		t.Fatalf("expected 2 messages per session, got %d and %d", len(firstMessages), len(secondMessages))
	}
	if secondMessages[0].Content != "second session" {
		t.Fatalf("second conversation = %#v", secondMessages)
	}
}

func TestSessionSummaryIsCumulative(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "I love Go"}},
		&memori.Message{Role: "assistant", Content: "Great"},
	)
	flushMemori(t, m)
	first := conversationSummary(t, m)
	if first == "" {
		t.Fatal("summary should be written")
	}

	writeConversation(t, m,
		[]memori.Message{
			{Role: "user", Content: "I love Go"},
			{Role: "assistant", Content: "Great"},
			{Role: "user", Content: "I live in Shanghai"},
		},
		&memori.Message{Role: "assistant", Content: "Noted"},
	)
	flushMemori(t, m)
	second := conversationSummary(t, m)
	if !strings.Contains(second, first) {
		t.Fatalf("summary should accumulate: %q -> %q", first, second)
	}
	if !strings.Contains(second, "Shanghai") {
		t.Fatalf("summary missing new content: %q", second)
	}
}

func TestReindexAfterEmbeddingDimensionChange(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}

	m.Embedder = embed.NewHashEmbedderWithDim(128)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := m.Reindex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reindexed = %d, want 1", n)
	}

	facts, err := m.Recall("favorite color", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || !strings.Contains(facts[0].Content, "blue") {
		t.Fatalf("recall after reindex = %#v", facts)
	}
}

func TestInvalidAttributionDoesNotPanicAndCanRecover(t *testing.T) {
	m := newTestMemori(t)
	long := strings.Repeat("x", 101)
	m.Attribution(long, "proc")
	if m.Err() == nil {
		t.Fatal("expected attribution error")
	}
	if _, err := m.Recall("anything", 1); err == nil {
		t.Fatal("operations should surface attribution error")
	}
	if err := m.TryAttribution("valid", "proc"); err != nil {
		t.Fatal(err)
	}
	if m.Err() != nil {
		t.Fatalf("error not cleared: %v", m.Err())
	}
	if _, err := m.Remember("I like tea"); err != nil {
		t.Fatal(err)
	}
}

func TestRememberRequiresAttribution(t *testing.T) {
	m := newTestMemori(t)
	if _, err := m.Remember("I like tea"); !errors.Is(err, memori.ErrNoAttribution) {
		t.Fatalf("err = %v, want ErrNoAttribution", err)
	}
}

func TestFlushAndClose(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "I love Go"}},
		&memori.Message{Role: "assistant", Content: "Great"},
	)

	flushMemori(t, m)
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := m.Recall("Go", 5); !errors.Is(err, memori.ErrClosed) {
		t.Fatalf("recall after close = %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestWriterDoesNotDuplicateResponseInHistory(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
		&memori.Message{Role: "assistant", Content: "hi there"},
	)
	flushMemori(t, m)

	msgs := recentStoredMessages(t, m)
	if len(msgs) != 2 {
		t.Fatalf("response duplicated: %#v", msgs)
	}
}

func TestEmptyQuerySkipsEmbedding(t *testing.T) {
	counter := &countingEmbedder{Embedder: embed.NewHashEmbedder()}
	m := newTestMemori(t, memori.WithEmbedder(counter))
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("I live in Shanghai"); err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)

	if _, err := m.Recall("", 5); err != nil {
		t.Fatal(err)
	}
	if n := counter.calls.Load(); n != 0 {
		t.Fatalf("empty query triggered %d embedding calls", n)
	}
}

type countingEmbedder struct {
	embed.Embedder
	calls atomic.Int64
}

func (c *countingEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	c.calls.Add(1)
	return c.Embedder.EmbedText(ctx, text)
}

func TestStorageMigrationIsIdempotent(t *testing.T) {
	m := newTestMemori(t)
	if err := m.Storage.Build(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
