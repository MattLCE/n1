package migrations

import (
	"database/sql"
	"fmt"
	"time"
)

// Migration represents a single database migration
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// Runner manages database migrations
type Runner struct {
	db         *sql.DB
	migrations []Migration
}

// NewRunner creates a new migrations runner
func NewRunner(db *sql.DB) *Runner {
	return &Runner{
		db:         db,
		migrations: []Migration{},
	}
}

// AddMigration adds a migration to the runner
func (r *Runner) AddMigration(version int, description, sql string) {
	r.migrations = append(r.migrations, Migration{
		Version:     version,
		Description: description,
		SQL:         sql,
	})
}

// ensureMigrationsTable creates the migrations table if it doesn't exist
func (r *Runner) ensureMigrationsTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS _migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL
		)
	`)
	return err
}

// getAppliedMigrations returns a map of already applied migration versions
func (r *Runner) getAppliedMigrations() (map[int]bool, error) {
	rows, err := r.db.Query("SELECT version FROM _migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

// Run executes all pending migrations
func (r *Runner) Run() error {
	// Ensure migrations table exists
	if err := r.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get applied migrations
	applied, err := r.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Sort migrations by version (they should already be in order as added)
	for _, migration := range r.migrations {
		// Skip if already applied
		if applied[migration.Version] {
			continue
		}

		// Begin transaction for this migration
		tx, err := r.db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w", migration.Version, err)
		}

		// Execute migration SQL
		if _, err := tx.Exec(migration.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %d: %w", migration.Version, err)
		}

		// Record migration as applied
		_, err = tx.Exec(
			"INSERT INTO _migrations (version, description, applied_at) VALUES (?, ?, ?)",
			migration.Version,
			migration.Description,
			time.Now().UTC(),
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
		}
	}

	return nil
}
