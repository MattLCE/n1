package sqlite

import (
	"database/sql"
	"fmt"

	// Ensure the driver is imported. The name "_" means we only want its side effects (registering the driver).
	_ "github.com/mattn/go-sqlite3"
)

// Open returns a standard handle to a potentially non-existent SQLite database file.
// Creates the file if it does not exist. This version does NOT handle encryption.
func Open(path string) (*sql.DB, error) {
	// Basic DSN for a file-based SQLite database.
	// Busy timeout is generally a good idea.
	// Foreign keys are often enabled by default or good practice.
	dsn := fmt.Sprintf(
		"file:%s?_busy_timeout=5000&_foreign_keys=on", // Use standard DSN, no encryption params
		path,
	)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql open failed: %w", err)
	}

	// Ping to verify the connection is alive immediately after opening.
	if err := db.Ping(); err != nil {
		_ = db.Close() // Close on error
		return nil, fmt.Errorf("db ping failed after open: %w", err)
	}

	// Return the standard sql.DB handle
	return db, nil
}
