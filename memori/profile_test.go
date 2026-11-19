package memori_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestProfileGroupsAndOrdersSections(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	for _, fact := range []string{
		"My name is Alice",
		"I live in Shanghai",
		"My favorite color is blue",
		"I use Go every day",
	} {
		if _, err := m.Remember(fact); err != nil {
			t.Fatalf("remember %q: %v", fact, err)
		}
	}

	profile, err := m.Profile(context.Background(), memori.ProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if profile.EntityID != "user" {
		t.Fatalf("entity=%q", profile.EntityID)
	}
	if profile.Tokens > profile.Budget {
		t.Fatalf("tokens %d exceed budget %d", profile.Tokens, profile.Budget)
	}
	if !strings.Contains(profile.Text, "<memory_profile>") || !strings.Contains(profile.Text, "</memory_profile>") {
		t.Fatalf("missing profile envelope: %q", profile.Text)
	}
	if !strings.Contains(profile.Text, "Alice") || !strings.Contains(profile.Text, "blue") {
		t.Fatalf("profile missing facts: %q", profile.Text)
	}
	if len(profile.Sections) < 3 {
		t.Fatalf("expected grouped sections, got %#v", profile.Sections)
	}
	want := []string{"Identity", "Preferences", "Skills"}
	for i, name := range want {
		if profile.Sections[i].Name != name {
			t.Fatalf("section %d = %q, want %q (%#v)", i, profile.Sections[i].Name, name, profile.Sections)
		}
	}
	if len(profile.Facts) != 4 {
		t.Fatalf("facts=%d, want 4", len(profile.Facts))
	}
}

func TestProfileExcludesDynamicFactsByDefault(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("Please remember that my current focus is the Memorigo launch"); err != nil {
		t.Fatal(err)
	}

	profile, err := m.Profile(context.Background(), memori.ProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(profile.Text, "Memorigo launch") {
		t.Fatalf("dynamic fact leaked into static profile: %q", profile.Text)
	}

	profile, err = m.Profile(context.Background(), memori.ProfileOptions{IncludeDynamic: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile.Text, "Memorigo launch") {
		t.Fatalf("dynamic fact missing with opt-in: %q", profile.Text)
	}
	if len(profile.Sections) != 1 || profile.Sections[0].Name != "Recent context" {
		t.Fatalf("unexpected dynamic section: %#v", profile.Sections)
	}
}

func TestProfileBudgetTruncates(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	long := strings.Repeat("project context and delivery notes ", 12)
	for i := 0; i < 12; i++ {
		if _, err := m.Remember("Please remember that " + long + string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}

	profile, err := m.Profile(context.Background(), memori.ProfileOptions{
		TokenBudget:    40,
		MaxFacts:       50,
		IncludeDynamic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Tokens > 40 {
		t.Fatalf("tokens %d exceed budget 40", profile.Tokens)
	}
	if len(profile.Facts) == 0 {
		t.Fatal("expected at least one profile fact")
	}
	if !profile.Truncated {
		t.Fatal("expected truncated profile")
	}
}

func TestProfileSupersededAndEntityIsolation(t *testing.T) {
	m := newTestMemori(t)
	_ = m.TryAttribution("user", "proc")
	if _, err := m.Remember("I live in Shanghai"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I live in Beijing"); err != nil {
		t.Fatal(err)
	}

	profile, err := m.Profile(context.Background(), memori.ProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(profile.Text, "Shanghai") {
		t.Fatalf("superseded fact present: %q", profile.Text)
	}
	if !strings.Contains(profile.Text, "Beijing") {
		t.Fatalf("latest fact missing: %q", profile.Text)
	}

	if err := m.TryAttribution("other", "proc"); err != nil {
		t.Fatal(err)
	}
	other, err := m.Profile(context.Background(), memori.ProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Facts) != 0 || other.Text != "" {
		t.Fatalf("entity isolation failed: %#v", other)
	}
}

func TestProfileWithoutAttribution(t *testing.T) {
	m := newTestMemori(t)
	_, err := m.Profile(context.Background(), memori.ProfileOptions{})
	if err != memori.ErrNoAttribution {
		t.Fatalf("err=%v, want ErrNoAttribution", err)
	}
}
