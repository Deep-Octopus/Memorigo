package memori_test

import (
	"strings"
	"testing"
)

func TestScopeIsolatesTheSameEntityID(t *testing.T) {
	m := newTestMemori(t)
	m.Attribution("user", "proc")
	if err := m.SetScope("acme", "support"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("The acme account id is 1001"); err != nil {
		t.Fatal(err)
	}

	if err := m.SetScope("other", "support"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("The other account id is 2002"); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Recall("account id", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "1001") || !containsFact(facts, "2002") {
		t.Fatalf("other scope saw acme memory: %#v", facts)
	}

	if err := m.SetScope("acme", "support"); err != nil {
		t.Fatal(err)
	}
	facts, err = m.Recall("account id", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "1001") || containsFact(facts, "2002") {
		t.Fatalf("acme scope leaked: %#v", facts)
	}

	if err := m.SetScope("", ""); err != nil {
		t.Fatal(err)
	}
	facts, err = m.Recall("account id", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "1001") || containsFact(facts, "2002") {
		t.Fatalf("unscoped recall crossed tenants: %#v", facts)
	}
}

func TestSetScopeRejectsLongIDs(t *testing.T) {
	m := newTestMemori(t)
	err := m.SetScope(strings.Repeat("o", 101), "app")
	if err == nil {
		t.Fatal("expected a length error")
	}
}
