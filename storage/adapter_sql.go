package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

type SQLAdapter struct {
	DB      *sql.DB
	dialect string
}

func (a *SQLAdapter) Dialect() string { return a.dialect }

func isSQLDB(conn any) bool {
	_, ok := conn.(*sql.DB)
	return ok
}

func newSQLAdapter(conn any) (Adapter, error) {
	db := conn.(*sql.DB)
	// best-effort dialect detection
	driver := db.Driver()
	name := strings.ToLower(fmt.Sprintf("%T", driver))
	dialect := "postgres"
	switch {
	case strings.Contains(name, "sqlite"):
		dialect = "sqlite"
	case strings.Contains(name, "pgx"), strings.Contains(name, "postgres"):
		dialect = "postgres"
	}
	return &SQLAdapter{DB: db, dialect: dialect}, nil
}


