package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRLSMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "off"},
		{in: "off", want: "off"},
		{in: "false", want: "off"},
		{in: "0", want: "off"},
		{in: "no", want: "off"},
		{in: "maybe", want: "off"},
		{in: "ON", want: "on"},
		{in: "true", want: "on"},
		{in: "1", want: "on"},
		{in: "yes", want: "on"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Setenv("MEMORI_RLS", tc.in)
			if got := rlsMode(); got != tc.want {
				t.Fatalf("rlsMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRLSPoliciesStayOutOfMigrations(t *testing.T) {
	sets := map[string]map[int][]string{
		"sqlite":   sqliteMigrations,
		"postgres": postgresMigrations,
	}
	banned := []string{
		"row level security",
		"create policy",
		"memorigo_tenant",
		"row_security",
		"memorigo_org_id",
	}
	for name, migrations := range sets {
		if maxSQLMigrationVersion(migrations) != 5 {
			t.Errorf("%s schema version = %d, want 5", name, maxSQLMigrationVersion(migrations))
		}
		for version, stmts := range migrations {
			for _, stmt := range stmts {
				lower := strings.ToLower(stmt)
				for _, word := range banned {
					if strings.Contains(lower, word) {
						t.Errorf("%s migration %d contains %q", name, version, word)
					}
				}
			}
		}
	}
}

func TestRLSProtectedTables(t *testing.T) {
	got := rlsProtectedTables()
	want := []string{
		"memori_entity",
		"memori_session",
		"memori_conversation",
		"memori_conversation_message",
		"memori_entity_fact",
		"memori_knowledge_graph",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("protected = %v", got)
	}
	for _, table := range []string{
		"memori_schema_version",
		"memori_process",
		"memori_process_attribute",
		"memori_subject",
		"memori_predicate",
		"memori_object",
	} {
		if expr := rlsUsingExpr(table); expr != "" {
			t.Fatalf("%s has a policy expression %q", table, expr)
		}
	}
	if !strings.Contains(rlsUsingExpr("memori_entity"), "app.memorigo_org_id") {
		t.Fatal("entity policy does not read the tenant setting")
	}
	if !strings.Contains(rlsUsingExpr("memori_session"), "entity_id IS NULL") {
		t.Fatal("session policy drops rows that have no entity")
	}
	if quoteIdent(`a"b`) != `"a""b"` {
		t.Fatalf("quoteIdent = %s", quoteIdent(`a"b`))
	}
}

func TestRLSOnSQLiteIsANoOp(t *testing.T) {
	t.Setenv("MEMORI_RLS", "on")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:rls_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatal(err)
	}
	setter, ok := mgr.Driver().(TenantSetter)
	if !ok {
		t.Fatal("sql driver does not implement TenantSetter")
	}
	setter.SetTenant("other", "app")
	repos := mgr.Driver().(Repos)
	ctx := context.Background()
	key := ScopedExternalID("acme", "app", "user")
	id, err := repos.Entity().Create(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repos.Entity().GetByExternalID(ctx, key)
	if err != nil || got != id {
		t.Fatalf("sqlite hid a row while MEMORI_RLS=on: id=%d got=%d err=%v", id, got, err)
	}
	driver := mgr.Driver().(*SQLDriver)
	org, app := driver.Tenant()
	if org != "other" || app != "app" {
		t.Fatalf("tenant = %q %q", org, app)
	}
}
