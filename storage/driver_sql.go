package storage

import (
	"database/sql"
	"fmt"
)

type SQLDriver struct {
	a       *SQLAdapter
	dialect string
	repos   *sqlRepos
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
	maxVersion := 1 // Currently only version 1

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

		// Update schema version
		var updateSQL string
		if d.dialect == "postgres" {
			if currentVersion == 0 {
				updateSQL = "INSERT INTO memori_schema_version (num) VALUES ($1)"
			} else {
				updateSQL = "UPDATE memori_schema_version SET num = $1"
			}
			_, err = tx.Exec(updateSQL, v)
		} else {
			if currentVersion == 0 {
				updateSQL = "INSERT INTO memori_schema_version (num) VALUES (?)"
			} else {
				updateSQL = "UPDATE memori_schema_version SET num = ?"
			}
			_, err = tx.Exec(updateSQL, v)
		}
		if err != nil {
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

// Helpers for future repos:
func (d *SQLDriver) db() *sql.DB { return d.a.DB }


