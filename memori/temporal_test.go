package memori_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestSupersedeClosesValidityWindow(t *testing.T) {
	m, db := newTestMemoriDB(t)
	m.Attribution("user", "proc")

	if _, err := m.Remember("I live in Beijing"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I live in Shanghai"); err != nil {
		t.Fatal(err)
	}

	rows := loadFactRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	var closed, open factRow
	for _, row := range rows {
		if strings.Contains(row.content, "Beijing") {
			closed = row
		}
		if strings.Contains(row.content, "Shanghai") {
			open = row
		}
	}
	if closed.status != "superseded" || closed.validTo == "" || closed.validFrom == "" {
		t.Fatalf("old fact was not closed: %#v", closed)
	}
	if open.status != "active" || open.validTo != "" || open.validFrom == "" {
		t.Fatalf("new fact should still be valid: %#v", open)
	}

	facts, err := m.Recall("where do I live", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "Beijing") || !containsFact(facts, "Shanghai") {
		t.Fatalf("recall = %#v", facts)
	}
}

func TestExtractionRecordsSourceMessage(t *testing.T) {
	m, db := newTestMemoriDB(t)
	m.Attribution("user", "proc")
	writeConversation(t, m,
		[]memori.Message{{Role: "user", Content: "My favorite color is teal"}},
		&memori.Message{Role: "assistant", Content: "Noted."},
	)
	flushMemori(t, m)

	rows := loadFactRows(t, db)
	var found bool
	for _, row := range rows {
		if !strings.Contains(row.content, "teal") {
			continue
		}
		found = true
		if row.messageID <= 0 {
			t.Fatalf("fact has no source message: %#v", row)
		}
		if row.assertedBy != "user" {
			t.Fatalf("extracted user fact asserted_by = %q", row.assertedBy)
		}
	}
	if !found {
		t.Fatalf("teal fact missing: %#v", rows)
	}
}

func TestInferredFactsStayOutOfRecall(t *testing.T) {
	m := newTestMemori(t)
	m.Attribution("user", "proc")
	if _, err := m.RememberWith("The user might enjoy jazz", memori.RememberOptions{
		AssertedBy: "inferred",
		Confidence: 0.4,
	}); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Recall("jazz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "jazz") {
		t.Fatalf("inferred fact was recalled: %#v", facts)
	}

	m.Config.Recall.IncludeInferred = true
	facts, err = m.Recall("jazz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "jazz") {
		t.Fatalf("opt-in recall missed inferred fact: %#v", facts)
	}
	if facts[0].AssertedBy != "inferred" || facts[0].Confidence != 0.4 {
		t.Fatalf("provenance = %#v", facts[0])
	}
}

type factRow struct {
	content    string
	status     string
	assertedBy string
	validFrom  string
	validTo    string
	messageID  int64
}

func loadFactRows(t *testing.T, db *sql.DB) []factRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT content, status, asserted_by, valid_from, valid_to, source_message_id
		FROM memori_entity_fact ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []factRow
	for rows.Next() {
		var row factRow
		var validFrom, validTo sql.NullString
		var messageID sql.NullInt64
		if err := rows.Scan(&row.content, &row.status, &row.assertedBy, &validFrom, &validTo, &messageID); err != nil {
			t.Fatal(err)
		}
		row.validFrom = strings.TrimSpace(validFrom.String)
		row.validTo = strings.TrimSpace(validTo.String)
		if messageID.Valid {
			row.messageID = messageID.Int64
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
