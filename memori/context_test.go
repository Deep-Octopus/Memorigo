package memori_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestBuildContextFactsBudgetAndCitations(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I live in Shanghai"); err != nil {
		t.Fatal(err)
	}

	pkg, err := m.BuildContext(context.Background(), "favorite color", memori.ContextOptions{
		TokenBudget: 120,
		FactLimit:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Facts) == 0 {
		t.Fatal("expected facts")
	}
	if pkg.Tokens > pkg.Budget {
		t.Fatalf("tokens %d exceed budget %d", pkg.Tokens, pkg.Budget)
	}
	if !strings.Contains(pkg.Text, "[fact:") {
		t.Fatalf("missing fact citation: %q", pkg.Text)
	}
	if len(pkg.Citations) != len(pkg.Facts) {
		t.Fatalf("citations=%d facts=%d", len(pkg.Citations), len(pkg.Facts))
	}
	for _, c := range pkg.Citations {
		if c.Kind != "fact" || c.Ref == "" {
			t.Fatalf("bad citation: %#v", c)
		}
	}
}

func TestBuildContextRawEvidence(t *testing.T) {
	m := newTestMemori(t, memori.WithConfig(func(c *memori.Config) {
		c.Extraction.Provider = "off"
	}))
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My sister's name is Alice and she lives in Paris"}},
		&memori.Message{Role: "assistant", Content: "Thanks for sharing."},
	)
	flushMemori(t, m)

	pkg, err := m.BuildContext(context.Background(), "sister name", memori.ContextOptions{
		TokenBudget: 400,
		IncludeRaw:  true,
		RawLimit:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Facts) != 0 {
		t.Fatalf("extraction was off, got facts: %#v", pkg.Facts)
	}
	if len(pkg.Messages) == 0 {
		t.Fatal("expected raw evidence")
	}
	if !strings.Contains(pkg.Text, "Alice") || !strings.Contains(pkg.Text, "[msg:") {
		t.Fatalf("raw evidence missing from text: %q", pkg.Text)
	}
	found := false
	for _, c := range pkg.Citations {
		if c.Kind == "message" && c.Ref != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("message citation missing: %#v", pkg.Citations)
	}
}

func TestBuildContextCrossSessionEvidence(t *testing.T) {
	m := newTestMemori(t, memori.WithConfig(func(c *memori.Config) {
		c.Extraction.Provider = "off"
	}))
	_ = m.TryAttribution("user", "proc")

	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My passport is kept in the blue drawer"}},
		&memori.Message{Role: "assistant", Content: "Noted."},
	)
	flushMemori(t, m)

	m.NewSession()
	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "Let's talk about something else"}},
		&memori.Message{Role: "assistant", Content: "Sure."},
	)
	flushMemori(t, m)

	pkg, err := m.BuildContext(context.Background(), "where is my passport", memori.ContextOptions{
		TokenBudget: 400,
		IncludeRaw:  true,
		RawLimit:    4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pkg.Text, "blue drawer") {
		t.Fatalf("cross-session evidence missing: %q", pkg.Text)
	}
}

func TestBuildContextTruncatesToBudget(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	long := strings.Repeat("我喜欢用Go写很多很多代码", 40)
	if _, err := m.Remember(long); err != nil {
		t.Fatal(err)
	}

	pkg, err := m.BuildContext(context.Background(), "Go", memori.ContextOptions{TokenBudget: 60})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Tokens > 60 {
		t.Fatalf("tokens %d exceed budget 60", pkg.Tokens)
	}
	if !pkg.Truncated {
		t.Fatal("expected truncated=true")
	}
}

func TestBuildContextIncludesProfileAndDedupes(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My name is Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I live in Shanghai"); err != nil {
		t.Fatal(err)
	}

	pkg, err := m.BuildContext(context.Background(), "what is my name", memori.ContextOptions{
		TokenBudget:    300,
		FactLimit:      5,
		IncludeProfile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ProfileCount != 2 || len(pkg.Profile) != 2 {
		t.Fatalf("profile count=%d facts=%d", pkg.ProfileCount, len(pkg.Profile))
	}
	if !strings.Contains(pkg.Text, "<memory_profile>") || !strings.Contains(pkg.Text, "Alice") {
		t.Fatalf("profile missing from context: %q", pkg.Text)
	}
	if pkg.Tokens > pkg.Budget {
		t.Fatalf("tokens %d exceed budget %d", pkg.Tokens, pkg.Budget)
	}

	profileIDs := make(map[int64]struct{}, len(pkg.Profile))
	for _, f := range pkg.Profile {
		profileIDs[f.ID] = struct{}{}
	}
	for _, f := range pkg.Facts {
		if _, ok := profileIDs[f.ID]; ok {
			t.Fatalf("fact %d duplicated in profile and query evidence", f.ID)
		}
	}

	// Profile facts are packed into the block and must be attributed.
	cited := make(map[int64]bool, len(pkg.Citations))
	for _, c := range pkg.Citations {
		if c.Kind == "fact" {
			cited[c.ID] = true
		}
	}
	for id := range profileIDs {
		if !cited[id] {
			t.Fatalf("profile fact %d missing from citations: %#v", id, pkg.Citations)
		}
	}
}

func TestBuildContextProfileCanBeDisabled(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My name is Alice"); err != nil {
		t.Fatal(err)
	}
	pkg, err := m.BuildContext(context.Background(), "name", memori.ContextOptions{
		TokenBudget: 300,
		FactLimit:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ProfileCount != 0 || strings.Contains(pkg.Text, "<memory_profile>") {
		t.Fatalf("profile should be opt-in for BuildContext: %#v", pkg)
	}
}

func TestBuildContextAbstention(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("My name is Alice"); err != nil {
		t.Fatal(err)
	}

	pkg, err := m.BuildContext(context.Background(), "quantum flux capacitor", memori.ContextOptions{
		TokenBudget:    300,
		IncludeProfile: true,
		MinCoverage:    0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.InsufficientEvidence {
		t.Fatalf("expected insufficient evidence, coverage=%.2f profile=%d", pkg.Coverage, pkg.ProfileCount)
	}

	pkg, err = m.BuildContext(context.Background(), "another unknown topic", memori.ContextOptions{
		TokenBudget: 300,
		MinCoverage: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.InsufficientEvidence {
		t.Fatalf("expected insufficient evidence without profile, coverage=%.2f", pkg.Coverage)
	}

	pkg, err = m.BuildContext(context.Background(), "my name", memori.ContextOptions{
		TokenBudget: 300,
		FactLimit:   5,
		MinCoverage: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.InsufficientEvidence {
		t.Fatalf("unexpected abstention for a covered query: coverage=%.2f text=%q", pkg.Coverage, pkg.Text)
	}
}

func TestBuildContextReservesBudgetForRawEvidence(t *testing.T) {
	m := newTestMemori(t, memori.WithConfig(func(c *memori.Config) {
		c.Extraction.Provider = "off"
	}))
	_ = m.TryAttribution("user", "proc")
	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My sister's name is Alice and she lives in Paris"}},
		&memori.Message{Role: "assistant", Content: "Noted."},
	)
	flushMemori(t, m)
	for i := 0; i < 6; i++ {
		long := strings.Repeat("my sister detail and family context ", 20)
		if _, err := m.Remember("Please remember that " + long + string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}

	pkg, err := m.BuildContext(context.Background(), "sister name", memori.ContextOptions{
		TokenBudget: 240,
		FactLimit:   6,
		IncludeRaw:  true,
		RawLimit:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Tokens > pkg.Budget {
		t.Fatalf("tokens %d exceed budget %d", pkg.Tokens, pkg.Budget)
	}
	if len(pkg.Messages) == 0 {
		t.Fatalf("raw evidence was crowded out: %q", pkg.Text)
	}
	if !strings.Contains(pkg.Text, "Alice") {
		t.Fatalf("raw evidence content missing: %q", pkg.Text)
	}
}

func TestBuildContextWithoutAttribution(t *testing.T) {
	m := newTestMemori(t)
	pkg, err := m.BuildContext(context.Background(), "anything", memori.ContextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Text != "" || len(pkg.Facts) != 0 || len(pkg.Messages) != 0 {
		t.Fatalf("expected empty package: %#v", pkg)
	}
}
