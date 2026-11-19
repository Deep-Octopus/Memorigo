package memori_test

import (
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/storage"
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

func TestSetScopePushesTenant(t *testing.T) {
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_ORG_ID": "from-env",
		"MEMORI_APP_ID": "billing",
	})
	driver, ok := m.Storage.Driver().(*storage.SQLDriver)
	if !ok {
		t.Fatalf("driver type %T", m.Storage.Driver())
	}
	org, app := driver.Tenant()
	if org != "from-env" || app != "billing" {
		t.Fatalf("startup tenant = %q %q", org, app)
	}
	if err := m.SetScope("acme", "support"); err != nil {
		t.Fatal(err)
	}
	org, app = driver.Tenant()
	if org != "acme" || app != "support" {
		t.Fatalf("SetScope tenant = %q %q", org, app)
	}
}

func TestSetScopeRejectsLongIDs(t *testing.T) {
	m := newTestMemori(t)
	err := m.SetScope(strings.Repeat("o", 101), "app")
	if err == nil {
		t.Fatal("expected a length error")
	}
}
