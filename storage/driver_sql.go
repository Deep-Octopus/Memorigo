package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

type SQLDriver struct {
	a       *SQLAdapter
	dialect string

	once sync.Once
	base Repos
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
		d.base = newSQLRepos(d.a.DB, d.dialect)
	})
	return d.base
}

func (d *SQLDriver) Entity() EntityRepo             { return d.baseRepos().Entity() }
func (d *SQLDriver) Process() ProcessRepo           { return d.baseRepos().Process() }
func (d *SQLDriver) Session() SessionRepo           { return d.baseRepos().Session() }
func (d *SQLDriver) Conversation() ConversationRepo { return d.baseRepos().Conversation() }
func (d *SQLDriver) Message() MessageRepo           { return d.baseRepos().Message() }
func (d *SQLDriver) EntityFact() EntityFactRepo     { return d.baseRepos().EntityFact() }

// Transaction runs fn inside a database transaction. Repositories handed to fn
// are bound to the transaction; any error rolls the whole unit back.
func (d *SQLDriver) Transaction(ctx context.Context, fn func(Repos) error) error {
	tx, err := d.a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(newSQLRepos(tx, d.dialect)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *SQLDriver) Migrate() error {
	if d.a == nil || d.a.DB == nil {
		return nil
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
		return nil
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

	return tx.Commit()
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
