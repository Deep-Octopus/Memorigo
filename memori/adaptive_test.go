package memori_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestDecideRecall(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		existing string
		enabled  bool
		coverage float64
		skip     bool
		reason   string
		want     string
	}{
		{name: "off keeps original", query: "  hi color  ", enabled: false, want: "  hi color  "},
		{name: "empty", query: "  ", enabled: true, skip: true, reason: "empty"},
		{name: "hello", query: "Hello!", enabled: true, skip: true, reason: "greeting"},
		{name: "nihao", query: "你好", enabled: true, skip: true, reason: "greeting"},
		{name: "thanks", query: "谢谢！", enabled: true, skip: true, reason: "greeting"},
		{name: "hello there", query: "hello there", enabled: true, skip: true, reason: "greeting"},
		{name: "stacked greeting", query: "hi, thanks, what's my favorite color", enabled: true, want: "what's my favorite color"},
		{name: "history stays", query: "history of rome", enabled: true, want: "history of rome"},
		{name: "covered question", query: "what's my favorite color?", existing: "Your favorite color is blue.", enabled: true, skip: true, reason: "sufficient", want: "what's my favorite color?"},
		{name: "partial coverage still searches", query: "garage code", existing: "I bought another notebook today", enabled: true, want: "garage code"},
		{name: "half the tokens stay", query: "garage code", existing: "the garage is big", enabled: true, coverage: 0.85, want: "garage code"},
		{name: "no existing text", query: "favorite color", existing: "  ", enabled: true, want: "favorite color"},
		{name: "chinese rewrite", query: "你好，我最喜欢的颜色", enabled: true, want: "我最喜欢的颜色"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := memori.DecideRecall(tc.query, tc.existing, tc.enabled, tc.coverage)
			if got.Skip != tc.skip || got.Reason != tc.reason || got.Query != tc.want {
				t.Fatalf("DecideRecall(%q, %q) = %+v, want skip=%v reason=%q query=%q", tc.query, tc.existing, got, tc.skip, tc.reason, tc.want)
			}
		})
	}
}

func TestAdaptiveConfigFromEnv(t *testing.T) {
	t.Setenv("MEMORI_RECALL_ADAPTIVE", "")
	t.Setenv("MEMORI_RECALL_ADAPTIVE_COVERAGE", "")
	m := memori.New()
	if !m.Config.Recall.Adaptive || m.Config.Recall.AdaptiveCoverage != 0.85 {
		t.Fatalf("defaults = %+v", m.Config.Recall)
	}
	t.Setenv("MEMORI_RECALL_ADAPTIVE", "off")
	t.Setenv("MEMORI_RECALL_ADAPTIVE_COVERAGE", "0.5")
	m = memori.New()
	if m.Config.Recall.Adaptive || m.Config.Recall.AdaptiveCoverage != 0.5 {
		t.Fatalf("env = %+v", m.Config.Recall)
	}
	t.Setenv("MEMORI_RECALL_ADAPTIVE_COVERAGE", "0")
	m = memori.New()
	if m.Config.Recall.AdaptiveCoverage != 0.85 {
		t.Fatalf("zero coverage should use 0.85, got %v", m.Config.Recall.AdaptiveCoverage)
	}
}

func TestAdaptiveRecallSkipsGreetingAndRewrites(t *testing.T) {
	counter := &queryEmbedder{Embedder: embed.NewHashEmbedder()}
	m := newTestMemori(t, memori.WithEmbedder(counter))
	if err := m.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("The garage code is 4821"); err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)
	counter.last.Store("")

	facts, err := m.Recall("hello", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 || counter.calls.Load() != 0 {
		t.Fatalf("greeting recall facts=%d embeds=%d", len(facts), counter.calls.Load())
	}

	facts, err = m.Recall("", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 || counter.calls.Load() != 0 {
		t.Fatalf("empty recall facts=%d embeds=%d", len(facts), counter.calls.Load())
	}

	facts, err = m.Recall("hi, what's my favorite color", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !factsContain(facts, "blue") {
		t.Fatalf("rewritten recall = %#v", facts)
	}
	if got := counter.last.Load().(string); got != "what's my favorite color" {
		t.Fatalf("embedded query = %q", got)
	}

	pkg, err := m.BuildContext(context.Background(), "hello", memori.ContextOptions{IncludeProfile: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.RecallSkipped || pkg.RecallSkipReason != "greeting" || len(pkg.Facts) != 0 {
		t.Fatalf("greeting context = skipped %v reason %q facts %d", pkg.RecallSkipped, pkg.RecallSkipReason, len(pkg.Facts))
	}
	if !strings.Contains(pkg.ProfileText, "blue") {
		t.Fatalf("profile dropped on greeting: %q", pkg.ProfileText)
	}

	before := counter.calls.Load()
	pkg, err = m.BuildContext(context.Background(), "what's my favorite color?", memori.ContextOptions{
		IncludeProfile: true,
		Existing:       "Your favorite color is blue.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.RecallSkipped || pkg.RecallSkipReason != "sufficient" || counter.calls.Load() != before {
		t.Fatalf("covered context skipped=%v reason=%q embeds %d->%d", pkg.RecallSkipped, pkg.RecallSkipReason, before, counter.calls.Load())
	}

	pkg, err = m.BuildContext(context.Background(), "garage code", memori.ContextOptions{
		Existing: "I bought another notebook today, copy 3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.RecallSkipped {
		t.Fatal("unrelated context skipped garage code")
	}
	if !factsContain(pkg.Facts, "4821") {
		t.Fatalf("garage code missing: %#v", pkg.Facts)
	}
}

func factsContain(facts []memori.Fact, needle string) bool {
	for _, fact := range facts {
		if strings.Contains(fact.Content, needle) {
			return true
		}
	}
	return false
}

func TestAdaptiveOffKeepsEmptyRecency(t *testing.T) {
	counter := &queryEmbedder{Embedder: embed.NewHashEmbedder()}
	m := newTestMemori(t, memori.WithEmbedder(counter), memori.WithConfig(func(c *memori.Config) {
		c.Recall.Adaptive = false
	}))
	if err := m.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)
	facts, err := m.Recall("", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || counter.calls.Load() != 0 {
		t.Fatalf("adaptive off empty recall facts=%d embeds=%d", len(facts), counter.calls.Load())
	}
	if _, err := m.Recall("hello", 5); err != nil {
		t.Fatal(err)
	}
	if counter.calls.Load() == 0 {
		t.Fatal("adaptive off should still embed a greeting")
	}
}

func TestExistingTurnsOmitsLatestUser(t *testing.T) {
	got := memori.ExistingTurns([]memori.ChatMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "My favorite color is blue"},
		{Role: "assistant", Content: "Noted."},
		{Role: "user", Content: "what's my favorite color?"},
	})
	if !strings.Contains(got, "blue") || !strings.Contains(got, "Noted") {
		t.Fatalf("prior turns = %q", got)
	}
	if strings.Contains(got, "what's my favorite color") || strings.Contains(got, "instructions") {
		t.Fatalf("latest user or system leaked: %q", got)
	}
}

type queryEmbedder struct {
	embed.Embedder
	calls atomic.Int64
	last  atomic.Value
}

func (q *queryEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	q.calls.Add(1)
	q.last.Store(text)
	return q.Embedder.EmbedText(ctx, text)
}
