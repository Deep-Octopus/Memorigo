package memori_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestRememberRuleStaysInProfileUntilForgotten(t *testing.T) {
	m := newTestMemori(t)
	if err := m.TryAttribution("rule-user", "rules"); err != nil {
		t.Fatal(err)
	}
	goID, err := m.RememberRule("Always answer in Go")
	if err != nil {
		t.Fatal(err)
	}
	tabID, err := m.RememberRule("Use tabs for indentation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("My favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RememberRule("  "); err == nil {
		t.Fatal("expected empty rule to fail")
	}

	profile, err := m.Profile(context.Background(), memori.ProfileOptions{TokenBudget: 400, MaxFacts: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile.Text, "Always answer in Go") || !strings.Contains(profile.Text, "Use tabs") {
		t.Fatalf("profile missed rules: %s", profile.Text)
	}
	if !hasSection(profile, "Rules") {
		t.Fatalf("sections = %+v", profile.Sections)
	}

	pkg, err := m.BuildContext(context.Background(), "what is 2 + 2", memori.ContextOptions{
		TokenBudget:    500,
		IncludeProfile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pkg.Text, "Always answer in Go") {
		t.Fatalf("unrelated query dropped the rule: %s", pkg.Text)
	}

	if err := m.Forget(goID); err != nil {
		t.Fatal(err)
	}
	profile, err = m.Profile(context.Background(), memori.ProfileOptions{TokenBudget: 400, MaxFacts: 20})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(profile.Text, "Always answer in Go") || !strings.Contains(profile.Text, "Use tabs") {
		t.Fatalf("rollback profile = %s", profile.Text)
	}
	facts, err := m.Facts(20)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.ID == goID {
			t.Fatalf("forgotten rule still active: %+v", fact)
		}
		if fact.ID == tabID && fact.Slot != memori.ProcedureSlot {
			t.Fatalf("rule slot = %q", fact.Slot)
		}
	}
}

func TestRememberDoesNotInventAProcedureSlot(t *testing.T) {
	m := newTestMemori(t)
	if err := m.TryAttribution("rule-user", "rules"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("Always answer in Go"); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Facts(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Slot == memori.ProcedureSlot {
			t.Fatalf("extraction stored a procedure: %+v", fact)
		}
	}
}

func hasSection(profile memori.Profile, name string) bool {
	for _, section := range profile.Sections {
		if section.Name == name {
			return true
		}
	}
	return false
}
