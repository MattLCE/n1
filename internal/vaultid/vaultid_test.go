package vaultid

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateVaultID(t *testing.T) {
	id1 := GenerateVaultID()
	id2 := GenerateVaultID()

	// Verify that generated IDs are not empty
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)

	// Verify that generated IDs are different
	assert.NotEqual(t, id1, id2)

	// Verify that generated IDs are valid UUIDs (36 characters)
	assert.Len(t, id1, 36)
	assert.Len(t, id2, 36)
}

func TestFormatSecretName(t *testing.T) {
	vaultID := "12345678-1234-1234-1234-123456789012"
	secretName := FormatSecretName(vaultID)

	// Verify that the secret name has the correct format
	assert.Equal(t, "n1_vault_12345678-1234-1234-1234-123456789012", secretName)
}

func TestEnsureVaultID(t *testing.T) {
	// Create a temporary database file
	tempDir, err := os.MkdirTemp("", "vaultid_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Ensure a vault ID is created
	vaultID1, err := EnsureVaultID(db)
	require.NoError(t, err)
	assert.NotEmpty(t, vaultID1)

	// Verify that the metadata table was created
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", MetadataTableName).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, MetadataTableName, tableName)

	// Verify that the vault ID was stored in the metadata table
	var storedID string
	err = db.QueryRow("SELECT value FROM metadata WHERE key=?", VaultIDKey).Scan(&storedID)
	require.NoError(t, err)
	assert.Equal(t, vaultID1, storedID)

	// Call EnsureVaultID again and verify that the same ID is returned
	vaultID2, err := EnsureVaultID(db)
	require.NoError(t, err)
	assert.Equal(t, vaultID1, vaultID2)
}

func TestGetVaultID(t *testing.T) {
	// Create a temporary database file
	tempDir, err := os.MkdirTemp("", "vaultid_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Try to get a vault ID from an empty database
	_, err = GetVaultID(db)
	assert.Error(t, err)

	// Create the metadata table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Try to get a vault ID from a database with no vault ID
	_, err = GetVaultID(db)
	assert.Error(t, err)

	// Insert a vault ID
	expectedID := "12345678-1234-1234-1234-123456789012"
	_, err = db.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", VaultIDKey, expectedID)
	require.NoError(t, err)

	// Get the vault ID
	vaultID, err := GetVaultID(db)
	require.NoError(t, err)
	assert.Equal(t, expectedID, vaultID)
}

func TestGetVaultIDFromPath(t *testing.T) {
	// Create a temporary database file
	tempDir, err := os.MkdirTemp("", "vaultid_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Create the metadata table and insert a vault ID
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	expectedID := "12345678-1234-1234-1234-123456789012"
	_, err = db.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", VaultIDKey, expectedID)
	require.NoError(t, err)

	db.Close()

	// Get the vault ID from the path
	vaultID, err := GetVaultIDFromPath(dbPath)
	require.NoError(t, err)
	assert.Equal(t, expectedID, vaultID)
}

func TestEnsureVaultIDFromPath(t *testing.T) {
	// Create a temporary database file
	tempDir, err := os.MkdirTemp("", "vaultid_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	db.Close()

	// Ensure a vault ID is created
	vaultID1, err := EnsureVaultIDFromPath(dbPath)
	require.NoError(t, err)
	assert.NotEmpty(t, vaultID1)

	// Call EnsureVaultIDFromPath again and verify that the same ID is returned
	vaultID2, err := EnsureVaultIDFromPath(dbPath)
	require.NoError(t, err)
	assert.Equal(t, vaultID1, vaultID2)
}
