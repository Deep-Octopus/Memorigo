package main

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// openInMemorySQLite returns an in-memory SQLite *sql.DB and a cleanup func.
func openInMemorySQLite() (*sql.DB, func()) {
	db, err := sql.Open("sqlite", "file:memori_demo?mode=memory&cache=shared")
	if err != nil {
		panic(err)
	}
	cleanup := func() { _ = db.Close() }
	return db, cleanup
}


