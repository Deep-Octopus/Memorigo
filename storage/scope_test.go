package storage

import "testing"

func TestScopedExternalIDRoundTrip(t *testing.T) {
	key := ScopedExternalID("acme", "support", "user-1")
	org, app, entity := ScopeParts(key)
	if org != "acme" || app != "support" || entity != "user-1" {
		t.Fatalf("parts = %q %q %q", org, app, entity)
	}
	if ScopedExternalID("", "", "user-1") != "user-1" {
		t.Fatal("unscoped key changed")
	}
	if _, _, entity := ScopeParts("user-1"); entity != "user-1" {
		t.Fatalf("plain entity = %q", entity)
	}
}
