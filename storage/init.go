package storage

import (
	_ "database/sql"
)

func init() {
	RegisterAdapter(isSQLDB, newSQLAdapter)
	RegisterAdapter(isMongoDB, newMongoAdapter)

	// drivers
	RegisterDriver("sqlite", newSQLDriver("sqlite"))
	RegisterDriver("postgres", newSQLDriver("postgres"))
	RegisterDriver("mongodb", newMongoDriver)

}


