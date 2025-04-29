package sqlite

import (
	// Import errors potentially if needed for specific error checks later
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require" // Using testify/require
)

// TestPlainOpen verifies that the simplified Open function can create,
// open, and allow basic operations on a standard SQLite file.
func TestPlainOpen(t *testing.T) {
	// Use TempDir for automatic cleanup of the test database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "plain_test.db")
	t.Logf("Plain database path: %s", dbPath)

	// --- 1. Test Creating and Opening ---
	db, err := Open(dbPath) // Use the simplified Open
	require.NoError(t, err, "PlainOpen: Opening new file failed")
	require.NotNil(t, db, "PlainOpen: DB handle should not be nil on successful open")

	// --- 2. Test Basic Operation (Create Table) ---
	_, err = db.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err, "PlainOpen: Creating test_table failed")

	// --- 3. Test Closing ---
	err = db.Close()
	require.NoError(t, err, "PlainOpen: Closing DB failed")

	// --- 4. Test Reopening Existing File ---
	dbReopen, err := Open(dbPath)
	require.NoError(t, err, "PlainOpen: Reopening existing file failed")
	require.NotNil(t, dbReopen, "PlainOpen: Reopened DB handle should not be nil")

	// --- 5. Test Basic Read after Reopen ---
	var count int
	err = dbReopen.QueryRow(`SELECT count(*) FROM test_table`).Scan(&count)
	require.NoError(t, err, "PlainOpen: Selecting count after reopen failed")
	require.Equal(t, 0, count, "PlainOpen: Expected count to be 0")

	// --- 6. Close Reopened DB ---
	err = dbReopen.Close()
	require.NoError(t, err, "PlainOpen: Closing reopened DB failed")

	t.Logf("PlainOpen test completed successfully.")
}
