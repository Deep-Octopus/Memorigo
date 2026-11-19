package memori_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestMaintainExpiresStaleNoiseAndRestoresIt(t *testing.T) {
	m := newTestMemori(t)
	m.Attribution("user", "proc")

	if _, err := m.Remember("I like jazz records"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I like jazz records"); err != nil {
		t.Fatal(err)
	}
	noiseID, err := m.Remember("A passing remark about the elevator music")
	if err != nil {
		t.Fatal(err)
	}

	preview, err := m.Maintain(memori.MaintainOptions{
		DryRun:       true,
		TTL:          time.Hour,
		ProtectTimes: 2,
		Now:          time.Now().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Expired) != 1 || preview.Expired[0] != noiseID {
		t.Fatalf("dry run = %#v, noise %d", preview, noiseID)
	}
	facts, err := m.Recall("elevator music", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "elevator") {
		t.Fatal("dry run wrote")
	}

	report, err := m.Maintain(memori.MaintainOptions{
		TTL:          time.Hour,
		ProtectTimes: 2,
		Now:          time.Now().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Expired) != 1 {
		t.Fatalf("expired = %#v", report.Expired)
	}
	facts, err = m.Recall("jazz elevator", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "elevator") || !containsFact(facts, "jazz") {
		t.Fatalf("after expire = %#v", facts)
	}

	if err := m.Restore(noiseID); err != nil {
		t.Fatal(err)
	}
	facts, err = m.Recall("elevator music", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "elevator") {
		t.Fatalf("restore missed noise: %#v", facts)
	}

	if err := m.Forget(noiseID); err != nil {
		t.Fatal(err)
	}
	err = m.Restore(noiseID)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("restore of a deleted fact = %v", err)
	}
}
