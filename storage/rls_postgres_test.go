package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestRLSPostgresScratchSchema talks to Postgres only when MEMORI_RLS_TEST_DSN
// is set. It never reads POSTGRES_DSN. The test creates a schema and drops
// it. go test ./... passes without this variable and without a network
// connection. A superuser or BYPASSRLS role skips unless the test can create
// a non-bypass owner, because those roles ignore FORCE ROW LEVEL SECURITY.
func TestRLSPostgresScratchSchema(t *testing.T) {
	dsn := os.Getenv("MEMORI_RLS_TEST_DSN")
	if dsn == "" {
		t.Skip("MEMORI_RLS_TEST_DSN is not set")
	}
	t.Setenv("MEMORI_RLS", "on")
	t.Setenv("MEMORI_PGVECTOR", "off")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	schema := "mrls_s_" + suffix
	var role string
	t.Cleanup(func() {
		_, _ = db.Exec(`RESET ROLE`)
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schema) + ` CASCADE`)
		if role != "" {
			_, _ = db.Exec(`DROP OWNED BY ` + quoteIdent(role))
			_, _ = db.Exec(`DROP ROLE IF EXISTS ` + quoteIdent(role))
		}
	})

	var bypass bool
	if err := db.QueryRow(`SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		t.Fatal(err)
	}
	if bypass {
		role = "mrls_r_" + suffix
		if _, err := db.Exec(`CREATE ROLE ` + quoteIdent(role) + ` NOSUPERUSER NOBYPASSRLS NOLOGIN`); err != nil {
			t.Skipf("connection bypasses RLS and CREATE ROLE failed: %v", err)
		}
		if _, err := db.Exec(`GRANT ` + quoteIdent(role) + ` TO CURRENT_USER`); err != nil {
			t.Fatal(err)
		}
	}
	createSchema := `CREATE SCHEMA ` + quoteIdent(schema)
	if role != "" {
		createSchema += ` AUTHORIZATION ` + quoteIdent(role)
	}
	if _, err := db.Exec(createSchema); err != nil {
		t.Fatal(err)
	}
	if role != "" {
		if _, err := db.Exec(`SET ROLE ` + quoteIdent(role)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`SET search_path TO ` + quoteIdent(schema)); err != nil {
		t.Fatal(err)
	}
	var stillBypass bool
	if err := db.QueryRow(`SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&stillBypass); err != nil {
		t.Fatal(err)
	}
	if stillBypass {
		t.Fatal("RLS test is connected as a role that bypasses row security")
	}

	mgr := NewManager()
	if err := mgr.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Build(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT num FROM memori_schema_version LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("schema version = %d, want 5", version)
	}
	var policies int
	if err := db.QueryRow(`
SELECT COUNT(*) FROM pg_policies
WHERE schemaname = current_schema() AND policyname = 'memorigo_tenant'`).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if policies != len(rlsProtectedTables()) {
		t.Fatalf("policies = %d, want %d", policies, len(rlsProtectedTables()))
	}
	assertRLS(t, db, "memori_entity", true)
	assertRLS(t, db, "memori_schema_version", false)
	assertRLS(t, db, "memori_process", false)
	assertRLS(t, db, "memori_subject", false)

	repos := mgr.Driver().(Repos)
	setter := mgr.Driver().(TenantSetter)
	txr := mgr.Driver().(Transactional)
	ctx := context.Background()

	setter.SetTenant("o'brien", "app")
	quotedKey := ScopedExternalID("o'brien", "app", "user")
	quotedID, err := repos.Entity().Create(ctx, quotedKey)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repos.Entity().GetByExternalID(ctx, quotedKey); err != nil || got != quotedID {
		t.Fatalf("quoted tenant read: id=%d got=%d err=%v", quotedID, got, err)
	}

	setter.SetTenant("acme", "app")
	acmeKey := ScopedExternalID("acme", "app", "user")
	acmeID, err := repos.Entity().Create(ctx, acmeKey)
	if err != nil {
		t.Fatal(err)
	}
	sessionUUID := uuid.New()
	if _, err := repos.Session().Create(ctx, &acmeID, nil, sessionUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.EntityFact().Upsert(ctx, acmeID, FactInput{
		Content:        "acme secret",
		Embedding:      EncodeEmbedding([]float32{1, 0}),
		Uniq:           "acme-secret",
		EmbeddingModel: "test",
		EmbeddingDim:   2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Process().Create(ctx, "shared-proc"); err != nil {
		t.Fatal(err)
	}

	setter.SetTenant("beta", "app")
	if _, err := repos.Entity().GetByExternalID(ctx, acmeKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta read acme by key: %v", err)
	}
	if _, err := repos.Session().GetByUUID(ctx, sessionUUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta read acme session: %v", err)
	}
	facts, err := repos.EntityFact().Candidates(ctx, acmeID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("beta saw acme facts: %#v", facts)
	}
	var byID int
	if err := queryAs(ctx, db, "beta", "app", `SELECT count(*) FROM memori_entity WHERE id = $1`, []any{acmeID}, &byID); err != nil {
		t.Fatal(err)
	}
	if byID != 0 {
		t.Fatalf("beta selected acme entity id %d", acmeID)
	}

	setter.SetTenant("acme", "app")
	if got, err := repos.Entity().GetByExternalID(ctx, acmeKey); err != nil || got != acmeID {
		t.Fatalf("acme read after switch: got=%d err=%v", got, err)
	}
	facts, err = repos.EntityFact().Candidates(ctx, acmeID, 10)
	if err != nil || len(facts) != 1 || facts[0].Content != "acme secret" {
		t.Fatalf("acme facts = %#v err=%v", facts, err)
	}

	setter.SetTenant("", "")
	legacyID, err := repos.Entity().Create(ctx, "legacy-user")
	if err != nil {
		t.Fatal(err)
	}
	emptySession := uuid.New()
	if _, err := repos.Session().Create(ctx, nil, nil, emptySession); err != nil {
		t.Fatal(err)
	}
	setter.SetTenant("acme", "app")
	if _, err := repos.Entity().GetByExternalID(ctx, "legacy-user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scoped tenant saw the empty scope: %v", err)
	}
	if _, err := repos.Session().GetByUUID(ctx, emptySession); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scoped tenant saw an entity-less session: %v", err)
	}
	setter.SetTenant("", "")
	if got, err := repos.Entity().GetByExternalID(ctx, "legacy-user"); err != nil || got != legacyID {
		t.Fatalf("empty scope read: got=%d err=%v", got, err)
	}

	subjectID, predicateID, objectID := insertVocab(t, db)
	insertEdge(t, db, "acme", "app", acmeID, subjectID, predicateID, objectID)
	betaKey := ScopedExternalID("beta", "app", "user")
	setter.SetTenant("beta", "app")
	betaID, err := repos.Entity().Create(ctx, betaKey)
	if err != nil {
		t.Fatal(err)
	}
	insertEdge(t, db, "beta", "app", betaID, subjectID, predicateID, objectID)
	setter.SetTenant("acme", "app")
	if err := txr.Transaction(ctx, func(tx Repos) error {
		_, err := tx.Subject().Delete(ctx, acmeID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var subjects, betaEdges, acmeEdges int
	if err := queryBypass(ctx, db, `SELECT count(*) FROM memori_subject`, nil, &subjects); err != nil {
		t.Fatal(err)
	}
	if subjects != 1 {
		t.Fatalf("vocabulary rows = %d, want 1", subjects)
	}
	if err := queryBypass(ctx, db, `SELECT count(*) FROM memori_knowledge_graph WHERE entity_id = $1`, []any{betaID}, &betaEdges); err != nil {
		t.Fatal(err)
	}
	if betaEdges != 1 {
		t.Fatalf("beta edges = %d", betaEdges)
	}
	if err := queryBypass(ctx, db, `SELECT count(*) FROM memori_knowledge_graph WHERE entity_id = $1`, []any{acmeID}, &acmeEdges); err != nil {
		t.Fatal(err)
	}
	if acmeEdges != 0 {
		t.Fatalf("acme edges = %d", acmeEdges)
	}

	var processes, leaked int
	if err := db.QueryRow(`SELECT count(*) FROM memori_process`).Scan(&processes); err != nil {
		t.Fatal(err)
	}
	if processes != 1 {
		t.Fatalf("process rows without a tenant setting = %d, want 1", processes)
	}
	if err := db.QueryRow(`SELECT count(*) FROM memori_entity`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("unset tenant saw %d entities; the GUC leaked or RLS is not forced", leaked)
	}

	t.Setenv("MEMORI_RLS", "off")
	mgr2 := NewManager()
	if err := mgr2.Start(db); err != nil {
		t.Fatal(err)
	}
	if err := mgr2.Build(); !errors.Is(err, ErrRLSPolicyPresent) {
		t.Fatalf("mode off with policies installed: %v", err)
	}
}

func assertRLS(t *testing.T, db *sql.DB, table string, want bool) {
	t.Helper()
	var enabled, forced bool
	err := db.QueryRow(`
SELECT c.relrowsecurity, c.relforcerowsecurity
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = current_schema() AND c.relname = $1`, table).Scan(&enabled, &forced)
	if err != nil {
		t.Fatal(err)
	}
	if enabled != want || forced != want {
		t.Fatalf("%s row security enabled=%v forced=%v, want %v", table, enabled, forced, want)
	}
}

func queryAs(ctx context.Context, db *sql.DB, org, app, query string, args []any, dest *int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('app.memorigo_org_id', $1, true), set_config('app.memorigo_app_id', $2, true)`,
		org, app); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, query, args...).Scan(dest)
}

func queryBypass(ctx context.Context, db *sql.DB, query string, args []any, dest *int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL row_security = off`); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, query, args...).Scan(dest)
}

func insertVocab(t *testing.T, db *sql.DB) (subjectID, predicateID, objectID int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Now()
	if err := tx.QueryRowContext(ctx, `
INSERT INTO memori_subject (uuid, name, type, uniq, date_created)
VALUES ($1, 'shared', 'thing', $2, $3) RETURNING id`,
		uuid.New().String(), uniq64("subject"), now).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `
INSERT INTO memori_predicate (uuid, content, uniq, date_created)
VALUES ($1, 'knows', $2, $3) RETURNING id`,
		uuid.New().String(), uniq64("predicate"), now).Scan(&predicateID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `
INSERT INTO memori_object (uuid, name, type, uniq, date_created)
VALUES ($1, 'shared-object', 'thing', $2, $3) RETURNING id`,
		uuid.New().String(), uniq64("object"), now).Scan(&objectID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return subjectID, predicateID, objectID
}

func insertEdge(t *testing.T, db *sql.DB, org, app string, entityID, subjectID, predicateID, objectID int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('app.memorigo_org_id', $1, true), set_config('app.memorigo_app_id', $2, true)`,
		org, app); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO memori_knowledge_graph
    (uuid, entity_id, subject_id, predicate_id, object_id, num_times, date_last_time, date_created)
VALUES ($1, $2, $3, $4, $5, 1, $6, $6)`,
		uuid.New().String(), entityID, subjectID, predicateID, objectID, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func uniq64(prefix string) string {
	if len(prefix) > 64 {
		return prefix[:64]
	}
	return prefix + "0000000000000000000000000000000000000000000000000000000000000000"[:64-len(prefix)]
}
