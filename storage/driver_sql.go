package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

type SQLDriver struct {
	a       *SQLAdapter
	dialect string

	once sync.Once
	base Repos

	// tenantMu guards the org/app pair copied into a transaction-local GUC
	// when Postgres RLS is on. Application-layer keys do not read it.
	tenantMu sync.RWMutex
	org      string
	app      string
}

func newSQLDriver(dialect string) driverFactory {
	return func(adapter Adapter) (Driver, error) {
		a, ok := adapter.(*SQLAdapter)
		if !ok {
			return nil, fmt.Errorf("sql driver expects *SQLAdapter, got %T", adapter)
		}
		return &SQLDriver{a: a, dialect: dialect}, nil
	}
}

func (d *SQLDriver) Dialect() string { return d.dialect }

func (d *SQLDriver) db() *sql.DB { return d.a.DB }

func (d *SQLDriver) baseRepos() Repos {
	d.once.Do(func() {
		d.base = newSQLRepos(d.a.DB, d.dialect, d)
	})
	return d.base
}

func (d *SQLDriver) Entity() EntityRepo             { return d.baseRepos().Entity() }
func (d *SQLDriver) Process() ProcessRepo           { return d.baseRepos().Process() }
func (d *SQLDriver) Session() SessionRepo           { return d.baseRepos().Session() }
func (d *SQLDriver) Conversation() ConversationRepo { return d.baseRepos().Conversation() }
func (d *SQLDriver) Message() MessageRepo           { return d.baseRepos().Message() }
func (d *SQLDriver) EntityFact() EntityFactRepo     { return d.baseRepos().EntityFact() }
func (d *SQLDriver) Subject() SubjectRepo           { return d.baseRepos().Subject() }

// Transaction runs fn inside a database transaction. Repositories handed to fn
// are bound to the transaction; any error rolls the whole unit back.
func (d *SQLDriver) Transaction(ctx context.Context, fn func(Repos) error) error {
	// Install policies before the callback transaction starts. Doing that
	// inside the callback deadlocks when the pool has one connection.
	if err := d.ensureRLS(ctx); err != nil {
		return err
	}
	tx, err := d.a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(newSQLRepos(tx, d.dialect, d)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *SQLDriver) Migrate() error {
	if d.a == nil || d.a.DB == nil {
		return nil
	}
	if err := d.rejectRLSIfDisabled(context.Background()); err != nil {
		return err
	}

	var migrations map[int][]string
	switch d.dialect {
	case "sqlite":
		migrations = sqliteMigrations
	case "postgres":
		migrations = postgresMigrations
	default:
		return fmt.Errorf("unsupported SQL dialect: %s", d.dialect)
	}

	currentVersion := d.getSchemaVersion()
	maxVersion := maxSQLMigrationVersion(migrations)
	if currentVersion >= maxVersion {
		if err := d.backfillSearchText(); err != nil {
			return err
		}
		if err := d.ensureRLS(context.Background()); err != nil {
			return err
		}
		return d.requirePGVector()
	}

	tx, err := d.a.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for v := currentVersion + 1; v <= maxVersion; v++ {
		ops, ok := migrations[v]
		if !ok {
			continue
		}
		for _, op := range ops {
			if _, err := tx.Exec(op); err != nil {
				return fmt.Errorf("migration %d failed: %w", v, err)
			}
		}

		var updateSQL string
		if currentVersion == 0 {
			updateSQL = "INSERT INTO memori_schema_version (num) VALUES (" + sqlPlaceholder(d.dialect, 1) + ")"
		} else {
			updateSQL = "UPDATE memori_schema_version SET num = " + sqlPlaceholder(d.dialect, 1)
		}
		if _, err := tx.Exec(updateSQL, v); err != nil {
			return err
		}
		currentVersion = v
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if err := d.backfillSearchText(); err != nil {
		return err
	}
	if err := d.ensureRLS(context.Background()); err != nil {
		return err
	}
	return d.requirePGVector()
}

// backfillSearchText fills the lexical document for rows written before the
// search column existed. Rows that already have a document are skipped.
// SQLite triggers mirror the column into FTS5 when it changes.
func (d *SQLDriver) backfillSearchText() error {
	ctx := context.Background()
	active, err := d.rlsActive(ctx)
	if err != nil {
		return err
	}
	// Policies hide every row when the tenant GUC is unset. Maintenance has
	// to see every tenant, and only the table owner can turn row_security off.
	run := func(q stdSQL) error {
		return backfillSearchTextOn(ctx, d.dialect, q)
	}
	if active {
		return bypassRLS(ctx, d.db(), run)
	}
	tx, err := d.db().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func backfillSearchTextOn(ctx context.Context, dialect string, q stdSQL) error {
	rows, err := q.QueryContext(ctx, `SELECT id, content FROM memori_entity_fact WHERE search_text = ''`)
	if err != nil {
		return err
	}
	type pending struct {
		id      int64
		content string
	}
	var batch []pending
	for rows.Next() {
		var row pending
		if err := rows.Scan(&row.id, &row.content); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil
	}

	stmt := fmt.Sprintf(
		`UPDATE memori_entity_fact SET search_text = %s WHERE id = %s AND search_text = ''`,
		sqlPlaceholder(dialect, 1), sqlPlaceholder(dialect, 2),
	)
	for _, row := range batch {
		doc := textutil.SearchDocument(row.content)
		if doc == "" {
			doc = " "
		}
		if _, err := q.ExecContext(ctx, stmt, doc, row.id); err != nil {
			return err
		}
	}
	return nil
}

func (d *SQLDriver) getSchemaVersion() int {
	var version sql.NullInt64
	err := d.a.DB.QueryRow("SELECT num FROM memori_schema_version LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows || !version.Valid {
		return 0
	}
	if err != nil {
		return 0
	}
	return int(version.Int64)
}

func maxSQLMigrationVersion(migrations map[int][]string) int {
	max := 0
	for v := range migrations {
		if v > max {
			max = v
		}
	}
	return max
}
