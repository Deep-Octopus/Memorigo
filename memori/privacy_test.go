package memori_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/pii"
)

func TestExportAndDeleteSubject(t *testing.T) {
	m, db := newTestMemoriDB(t)
	m.Attribution("alice", "proc")
	if _, err := m.Remember("Email ada@example.com about jazz records"); err != nil {
		t.Fatal(err)
	}
	writeConversation(t, m, []memori.Message{{
		Role:    "user",
		Content: "My mobile is 13800138000",
	}}, nil)

	m.Attribution("bob", "proc")
	if _, err := m.Remember("I like quiet trains"); err != nil {
		t.Fatal(err)
	}

	m.Attribution("alice", "proc")
	exported, err := m.Export(memori.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if exported.Version != 1 || exported.EntityID != "alice" {
		t.Fatalf("header = %+v", exported)
	}
	if !strings.Contains(text, "ada@example.com") || !strings.Contains(text, "13800138000") {
		t.Fatalf("export missed subject data: %s", text)
	}
	if strings.Contains(text, "quiet trains") {
		t.Fatalf("export leaked another subject: %s", text)
	}

	masked, err := m.Export(memori.ExportOptions{Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	maskedText, _ := json.Marshal(masked)
	if strings.Contains(string(maskedText), "ada@example.com") || strings.Contains(string(maskedText), "13800138000") {
		t.Fatalf("redacted export kept secrets: %s", maskedText)
	}
	if !strings.Contains(string(maskedText), pii.EmailToken) || !strings.Contains(string(maskedText), pii.PhoneToken) {
		t.Fatalf("redacted export = %s", maskedText)
	}

	report, err := m.DeleteSubject()
	if err != nil {
		t.Fatal(err)
	}
	if report.Entities != 1 || report.Facts < 1 || report.Messages < 1 {
		t.Fatalf("deletion = %+v", report)
	}
	again, err := m.DeleteSubject()
	if err != nil {
		t.Fatal(err)
	}
	if again.Entities != 0 || again.Facts != 0 {
		t.Fatalf("second deletion = %+v", again)
	}

	var facts, messages int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memori_entity_fact WHERE entity_id IN (SELECT id FROM memori_entity WHERE external_id = 'alice')`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM memori_conversation_message`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if facts != 0 || messages != 0 {
		t.Fatalf("rows left: facts %d messages %d", facts, messages)
	}

	m.Attribution("bob", "proc")
	bob, err := m.Recall("trains", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(bob, "train") {
		t.Fatalf("bob was deleted: %#v", bob)
	}

	m.Attribution("alice", "proc")
	if _, err := m.Remember("I like jazz records"); err != nil {
		t.Fatal(err)
	}
	alice, err := m.Recall("jazz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(alice, "jazz") {
		t.Fatalf("alice could not be recreated: %#v", alice)
	}
}

func TestWriteTimePIIRedact(t *testing.T) {
	m := newTestMemori(t, memori.WithConfig(func(c *memori.Config) {
		c.Privacy.Redact = true
	}))
	m.Attribution("user", "proc")
	if _, err := m.Remember("Email ada@example.com about jazz records"); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Recall("jazz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || strings.Contains(facts[0].Content, "ada@example.com") || !strings.Contains(facts[0].Content, pii.EmailToken) {
		t.Fatalf("stored fact = %#v", facts)
	}

	writeConversation(t, m, []memori.Message{{
		Role:    "user",
		Content: "My card is 4111-1111-1111-1111",
	}}, nil)
	exported, err := m.Export(memori.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(exported)
	if strings.Contains(string(blob), "4111") || !strings.Contains(string(blob), pii.CardToken) {
		t.Fatalf("message redact = %s", blob)
	}
}
