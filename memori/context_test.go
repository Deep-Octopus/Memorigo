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
