package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // Import SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations(t *testing.T) {
	// Create a temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "migrations_test.db")
	t.Logf("Test database path: %s", dbPath)

	// Open the database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err, "Opening database failed")
	defer db.Close()

	// Create a migrations runner
	runner := NewRunner(db)

	// Add test migrations
	runner.AddMigration(1, "Create test table", `
		CREATE TABLE test_table (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)
	`)
	runner.AddMigration(2, "Add column to test table", `
		ALTER TABLE test_table ADD COLUMN description TEXT
	`)

	// Run migrations
	err = runner.Run()
	require.NoError(t, err, "Running migrations failed")

	// Verify migrations table exists and has entries
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	require.NoError(t, err, "Counting migrations failed")
	assert.Equal(t, 2, count, "Expected 2 migrations to be recorded")

	// Verify test_table exists with the expected schema
	_, err = db.Exec("INSERT INTO test_table (id, name, description) VALUES (1, 'Test', 'Description')")
	require.NoError(t, err, "Inserting into test_table failed")

	// Test idempotence - running migrations again should not error
	err = runner.Run()
	require.NoError(t, err, "Re-running migrations failed")

	// Verify still only 2 migrations recorded
	err = db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	require.NoError(t, err, "Counting migrations after re-run failed")
	assert.Equal(t, 2, count, "Expected still 2 migrations to be recorded")

	// Add a new migration and run again
	runner.AddMigration(3, "Add another column", `
		ALTER TABLE test_table ADD COLUMN created_at TIMESTAMP
	`)

	err = runner.Run()
	require.NoError(t, err, "Running with new migration failed")

	// Verify now 3 migrations recorded
	err = db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	require.NoError(t, err, "Counting migrations after adding new one failed")
	assert.Equal(t, 3, count, "Expected 3 migrations to be recorded")

	// Verify the new column exists
	_, err = db.Exec("UPDATE test_table SET created_at = CURRENT_TIMESTAMP WHERE id = 1")
	require.NoError(t, err, "Updating with new column failed")
}

func TestBootstrapVault(t *testing.T) {
	// Create a temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault_test.db")
	t.Logf("Vault test database path: %s", dbPath)

	// Open the database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err, "Opening database failed")
	defer db.Close()

	// Bootstrap the vault
	err = BootstrapVault(db)
	require.NoError(t, err, "Bootstrapping vault failed")

	// Verify vault table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM vault").Scan(&count)
	require.NoError(t, err, "Counting vault records failed")
	assert.Equal(t, 0, count, "Expected empty vault table")

	// Verify index exists
	var indexExists bool
	err = db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sqlite_master 
			WHERE type='index' AND name='idx_vault_key'
		)
	`).Scan(&indexExists)
	require.NoError(t, err, "Checking index failed")
	assert.True(t, indexExists, "Expected vault key index to exist")

	// Test trigger by inserting and updating a record
	_, err = db.Exec("INSERT INTO vault (key, value) VALUES ('test_key', 'test_value')")
	require.NoError(t, err, "Inserting into vault failed")

	// Get the initial updated_at value
	var initialUpdatedAt string
	err = db.QueryRow("SELECT updated_at FROM vault WHERE key = 'test_key'").Scan(&initialUpdatedAt)
	require.NoError(t, err, "Getting initial updated_at failed")

	// Wait a moment to ensure timestamp would change
	t.Log("Waiting a moment before update...")
	time.Sleep(time.Second) // Add a 1-second delay

	// Update the record
	_, err = db.Exec("UPDATE vault SET value = 'new_value' WHERE key = 'test_key'")
	require.NoError(t, err, "Updating vault failed")

	// Get the new updated_at value
	var newUpdatedAt string
	err = db.QueryRow("SELECT updated_at FROM vault WHERE key = 'test_key'").Scan(&newUpdatedAt)
	require.NoError(t, err, "Getting new updated_at failed")

	// Verify updated_at changed
	assert.NotEqual(t, initialUpdatedAt, newUpdatedAt, "Expected updated_at to change after update")
}
