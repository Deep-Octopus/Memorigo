package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

const rlsPolicyName = "memorigo_tenant"

// ErrRLSPolicyPresent is returned when MEMORI_RLS is off but this schema
// already has the optional tenant policy. Leaving the policy in place and
// skipping the tenant setting would hide every row. The driver does not drop
// policies on its own.
var ErrRLSPolicyPresent = errors.New("storage: Postgres RLS policies are installed; set MEMORI_RLS=on or drop policy memorigo_tenant")

// stdSQL is the database/sql surface *sql.DB and *sql.Tx already share.
type stdSQL interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// plainQuerier adapts that surface to sqlQuerier. Postgres uses rlsQuerier
// instead, because a tenant setting has to ride on the same connection as
// the statement that follows it.
type plainQuerier struct{ std stdSQL }

func (p plainQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.std.ExecContext(ctx, query, args...)
}

func (p plainQuerier) QueryContext(ctx context.Context, query string, args ...any) (sqlRows, error) {
	return p.std.QueryContext(ctx, query, args...)
}

func (p plainQuerier) QueryRowContext(ctx context.Context, query string, args ...any) sqlRow {
	return p.std.QueryRowContext(ctx, query, args...)
}

type rlsDBState struct {
	mu    sync.Mutex
	ready bool
}

var rlsStates sync.Map

func rlsMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MEMORI_RLS"))) {
	case "on", "true", "1", "yes":
		return "on"
	default:
		return "off"
	}
}

func rlsStateFor(db *sql.DB) *rlsDBState {
	value, _ := rlsStates.LoadOrStore(db, &rlsDBState{})
	return value.(*rlsDBState)
}

// SetTenant records the org/app pair used by Postgres RLS. An empty pair is
// the historical single-tenant scope. Concurrent Memori values must not share
// one driver when they serve different tenants; ScopedExternalID remains the
// isolation that does not depend on this field.
func (d *SQLDriver) SetTenant(org, app string) {
	if d == nil {
		return
	}
	d.tenantMu.Lock()
	d.org, d.app = org, app
	d.tenantMu.Unlock()
}

// Tenant returns the org and app last passed to SetTenant.
func (d *SQLDriver) Tenant() (org, app string) {
	if d == nil {
		return "", ""
	}
	d.tenantMu.RLock()
	defer d.tenantMu.RUnlock()
	return d.org, d.app
}

func (d *SQLDriver) tenantSnapshot() (string, string) {
	if d == nil {
		return "", ""
	}
	d.tenantMu.RLock()
	defer d.tenantMu.RUnlock()
	return d.org, d.app
}

func (d *SQLDriver) rejectRLSIfDisabled(ctx context.Context) error {
	if d.dialect != "postgres" || rlsMode() == "on" || d.db() == nil {
		return nil
	}
	present, err := rlsPolicyPresent(ctx, d.db())
	if err != nil {
		return err
	}
	if present {
		return ErrRLSPolicyPresent
	}
	return nil
}

func (d *SQLDriver) rlsActive(ctx context.Context) (bool, error) {
	if d.dialect != "postgres" || rlsMode() != "on" || d.db() == nil {
		return false, nil
	}
	return rlsPolicyPresent(ctx, d.db())
}

func rlsPolicyPresent(ctx context.Context, q stdSQL) (bool, error) {
	var present bool
	err := q.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_policies
    WHERE schemaname = current_schema() AND policyname = $1
)`, rlsPolicyName).Scan(&present)
	return present, err
}

// ensureRLS installs FORCE ROW LEVEL SECURITY once for this *sql.DB. The
// statements are not part of the versioned migration: SQLite would reject
// them, and a database without MEMORI_RLS=on must stay unchanged. The fast
// path takes no connection, so a callback that already holds the only pooled
// connection can call it again.
func (d *SQLDriver) ensureRLS(ctx context.Context) error {
	if d == nil || d.dialect != "postgres" || rlsMode() != "on" || d.db() == nil {
		return nil
	}
	state := rlsStateFor(d.db())
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.ready {
		return nil
	}
	if err := installRLS(ctx, d.db()); err != nil {
		return err
	}
	state.ready = true
	return nil
}

func installRLS(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var schema sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return err
	}
	if !schema.Valid || schema.String == "" {
		return errors.New("storage: MEMORI_RLS=on requires current_schema() to be set")
	}
	for _, table := range rlsProtectedTables() {
		expr := rlsUsingExpr(table)
		qual := quoteIdent(schema.String) + "." + quoteIdent(table)
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+qual+` ENABLE ROW LEVEL SECURITY`); err != nil {
			return fmt.Errorf("storage: MEMORI_RLS=on could not enable row-level security (the role must own %s): %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+qual+` FORCE ROW LEVEL SECURITY`); err != nil {
			return fmt.Errorf("storage: MEMORI_RLS=on could not force row-level security on %s: %w", table, err)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_policies
    WHERE schemaname = $1 AND tablename = $2 AND policyname = $3
)`, schema.String, table, rlsPolicyName).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		stmt := `CREATE POLICY ` + quoteIdent(rlsPolicyName) + ` ON ` + qual +
			` USING (` + expr + `) WITH CHECK (` + expr + `)`
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("storage: MEMORI_RLS=on could not create policy on %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func rlsProtectedTables() []string {
	return []string{
		"memori_entity",
		"memori_session",
		"memori_conversation",
		"memori_conversation_message",
		"memori_entity_fact",
		"memori_knowledge_graph",
	}
}

func rlsUsingExpr(table string) string {
	org := `current_setting('app.memorigo_org_id', true)`
	app := `current_setting('app.memorigo_app_id', true)`
	match := `e.org_id = ` + org + ` AND e.app_id = ` + app
	empty := org + ` = '' AND ` + app + ` = ''`
	switch table {
	case "memori_entity":
		return `org_id = ` + org + ` AND app_id = ` + app
	case "memori_session":
		return `(entity_id IS NOT NULL AND EXISTS (SELECT 1 FROM memori_entity e WHERE e.id = entity_id AND ` + match + `)) OR (entity_id IS NULL AND ` + empty + `)`
	case "memori_conversation":
		return `EXISTS (SELECT 1 FROM memori_session s LEFT JOIN memori_entity e ON e.id = s.entity_id WHERE s.id = session_id AND ((s.entity_id IS NOT NULL AND ` + match + `) OR (s.entity_id IS NULL AND ` + empty + `)))`
	case "memori_conversation_message":
		return `EXISTS (SELECT 1 FROM memori_conversation c JOIN memori_session s ON s.id = c.session_id LEFT JOIN memori_entity e ON e.id = s.entity_id WHERE c.id = conversation_id AND ((s.entity_id IS NOT NULL AND ` + match + `) OR (s.entity_id IS NULL AND ` + empty + `)))`
	case "memori_entity_fact", "memori_knowledge_graph":
		return `EXISTS (SELECT 1 FROM memori_entity e WHERE e.id = entity_id AND ` + match + `)`
	default:
		return ""
	}
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// bypassRLS runs fn on one transaction with row_security off. SET LOCAL dies
// with the transaction, so the pooled connection does not keep the bypass.
// A role that is not the table owner gets an error from Postgres instead of
// a partial view of other tenants.
func bypassRLS(ctx context.Context, db *sql.DB, fn func(stdSQL) error) error {
	if db == nil {
		return errors.New("storage: no database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL row_security = off`); err != nil {
		return fmt.Errorf("storage: MEMORI_RLS=on cannot bypass row security for maintenance: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func applyTenant(ctx context.Context, q stdSQL, org, app string) error {
	_, err := q.ExecContext(ctx,
		`SELECT set_config('app.memorigo_org_id', $1, true), set_config('app.memorigo_app_id', $2, true)`,
		org, app)
	if err != nil {
		return fmt.Errorf("storage: setting Postgres tenant: %w", err)
	}
	return nil
}

// rlsQuerier pins the tenant to the same transaction as the statement. A
// session-level setting on a pooled connection would leak into the next
// tenant, or land on a different connection than the query.
type rlsQuerier struct {
	std    stdSQL
	driver *SQLDriver
}

func (q *rlsQuerier) active() bool {
	return q != nil && q.driver != nil && q.driver.dialect == "postgres" && rlsMode() == "on"
}

func (q *rlsQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !q.active() {
		return q.std.ExecContext(ctx, query, args...)
	}
	org, app := q.driver.tenantSnapshot()
	if tx, ok := q.std.(*sql.Tx); ok {
		if err := applyTenant(ctx, tx, org, app); err != nil {
			return nil, err
		}
		return tx.ExecContext(ctx, query, args...)
	}
	if err := q.driver.ensureRLS(ctx); err != nil {
		return nil, err
	}
	db, ok := q.std.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("storage: row-level security requires *sql.DB or *sql.Tx, got %T", q.std)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := applyTenant(ctx, tx, org, app); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func (q *rlsQuerier) QueryContext(ctx context.Context, query string, args ...any) (sqlRows, error) {
	if !q.active() {
		return q.std.QueryContext(ctx, query, args...)
	}
	org, app := q.driver.tenantSnapshot()
	if tx, ok := q.std.(*sql.Tx); ok {
		if err := applyTenant(ctx, tx, org, app); err != nil {
			return nil, err
		}
		return tx.QueryContext(ctx, query, args...)
	}
	if err := q.driver.ensureRLS(ctx); err != nil {
		return nil, err
	}
	db, ok := q.std.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("storage: row-level security requires *sql.DB or *sql.Tx, got %T", q.std)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := applyTenant(ctx, tx, org, app); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return &rlsRows{Rows: rows, tx: tx}, nil
}

func (q *rlsQuerier) QueryRowContext(ctx context.Context, query string, args ...any) sqlRow {
	if !q.active() {
		return q.std.QueryRowContext(ctx, query, args...)
	}
	return &rlsRow{ctx: ctx, q: q, query: query, args: args}
}

// ExecBypass runs one statement with row_security off, then turns it back on
// when the statement shares a caller transaction. Graph-vocabulary cleanup
// uses this so one tenant cannot treat another tenant's hidden edges as
// unreferenced. A superuser or BYPASSRLS role does not need the switch and
// still sees every row.
func (q *rlsQuerier) ExecBypass(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !q.active() {
		return q.std.ExecContext(ctx, query, args...)
	}
	if tx, ok := q.std.(*sql.Tx); ok {
		return execBypassOn(ctx, tx, query, args...)
	}
	if err := q.driver.ensureRLS(ctx); err != nil {
		return nil, err
	}
	db, ok := q.std.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("storage: row-level security bypass requires *sql.DB or *sql.Tx, got %T", q.std)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := execBypassOn(ctx, tx, query, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func execBypassOn(ctx context.Context, tx stdSQL, query string, args ...any) (sql.Result, error) {
	if _, err := tx.ExecContext(ctx, `SET LOCAL row_security = off`); err != nil {
		return nil, fmt.Errorf("storage: MEMORI_RLS=on cannot bypass row security for maintenance: %w", err)
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if _, restoreErr := tx.ExecContext(ctx, `SET LOCAL row_security = on`); restoreErr != nil && err == nil {
		err = fmt.Errorf("storage: restoring row security: %w", restoreErr)
	}
	return res, err
}

type rlsRows struct {
	*sql.Rows
	tx *sql.Tx
}

func (r *rlsRows) Close() error {
	var err error
	if r.Rows != nil {
		err = r.Rows.Close()
		r.Rows = nil
	}
	if r.tx != nil {
		rbErr := r.tx.Rollback()
		r.tx = nil
		if err == nil && rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = rbErr
		}
	}
	return err
}

type rlsRow struct {
	ctx   context.Context
	q     *rlsQuerier
	query string
	args  []any
}

func (r *rlsRow) Scan(dest ...any) error {
	org, app := r.q.driver.tenantSnapshot()
	if tx, ok := r.q.std.(*sql.Tx); ok {
		if err := applyTenant(r.ctx, tx, org, app); err != nil {
			return err
		}
		return tx.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...)
	}
	if err := r.q.driver.ensureRLS(r.ctx); err != nil {
		return err
	}
	db, ok := r.q.std.(*sql.DB)
	if !ok {
		return fmt.Errorf("storage: row-level security requires *sql.DB or *sql.Tx, got %T", r.q.std)
	}
	tx, err := db.BeginTx(r.ctx, nil)
	if err != nil {
		return err
	}
	if err := applyTenant(r.ctx, tx, org, app); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func sqlDB(q sqlQuerier) (*sql.DB, bool) {
	switch v := q.(type) {
	case plainQuerier:
		db, ok := v.std.(*sql.DB)
		return db, ok
	case *rlsQuerier:
		db, ok := v.std.(*sql.DB)
		return db, ok
	default:
		return nil, false
	}
}

var (
	_ sqlQuerier   = plainQuerier{}
	_ sqlQuerier   = (*rlsQuerier)(nil)
	_ TenantSetter = (*SQLDriver)(nil)
)
