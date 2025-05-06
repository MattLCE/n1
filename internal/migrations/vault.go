package migrations

import "database/sql"

// InitVaultMigrations adds the initial migrations for the vault table
func InitVaultMigrations(runner *Runner) {
	// Migration 1: Create the vault table
	runner.AddMigration(
		1,
		"Create vault table",
		`CREATE TABLE vault (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL,
			value BLOB NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	)

	// Migration 2: Create index on vault key
	runner.AddMigration(
		2,
		"Create index on vault key",
		`CREATE UNIQUE INDEX idx_vault_key ON vault(key)`,
	)

	// Migration 3: Create trigger to update the updated_at timestamp
	runner.AddMigration(
		3,
		"Create trigger for updated_at",
		`CREATE TRIGGER trig_vault_updated_at
		AFTER UPDATE ON vault
		BEGIN
			UPDATE vault SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END`,
	)

	// Migration 4: Create metadata table for vault information
	runner.AddMigration(
		4,
		"Create metadata table",
		`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	)
}

// BootstrapVault initializes the vault table in the database
func BootstrapVault(db *sql.DB) error {
	runner := NewRunner(db)
	InitVaultMigrations(runner)
	return runner.Run()
}
