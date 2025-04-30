package dao

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/n1/n1/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	// Create a temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault_dao_test.db")
	t.Logf("Test database path: %s", dbPath)

	// Open the database
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err, "Opening database failed")

	// Create the vault table
	_, err = db.Exec(`
		CREATE TABLE vault (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL,
			value BLOB NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX idx_vault_key ON vault(key);
		CREATE TRIGGER trig_vault_updated_at 
		AFTER UPDATE ON vault
		BEGIN
			UPDATE vault SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`)
	require.NoError(t, err, "Creating vault table failed")

	return db
}

func TestVaultDAO(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	dao := NewVaultDAO(db)

	// Test Get on non-existent key
	_, err := dao.Get("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound, "Expected ErrNotFound for non-existent key")

	// Test Put (insert)
	testKey := "test_key"
	testValue := []byte("test_value")
	err = dao.Put(testKey, testValue)
	require.NoError(t, err, "Put failed")

	// Test Get
	record, err := dao.Get(testKey)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, testKey, record.Key, "Key mismatch")
	assert.Equal(t, testValue, record.Value, "Value mismatch")
	assert.False(t, record.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, record.UpdatedAt.IsZero(), "UpdatedAt should be set")

	// Test Put (update)
	updatedValue := []byte("updated_value")
	err = dao.Put(testKey, updatedValue)
	require.NoError(t, err, "Update failed")

	// Test Get after update
	updatedRecord, err := dao.Get(testKey)
	require.NoError(t, err, "Get after update failed")
	assert.Equal(t, updatedValue, updatedRecord.Value, "Updated value mismatch")
	assert.Equal(t, record.CreatedAt, updatedRecord.CreatedAt, "CreatedAt should not change")
	assert.True(t, updatedRecord.UpdatedAt.After(record.UpdatedAt) ||
		updatedRecord.UpdatedAt.Equal(record.UpdatedAt),
		"UpdatedAt should be >= original")

	// Test List
	keys, err := dao.List()
	require.NoError(t, err, "List failed")
	assert.Contains(t, keys, testKey, "List should contain the test key")
	assert.Len(t, keys, 1, "List should contain exactly one key")

	// Test Delete
	err = dao.Delete(testKey)
	require.NoError(t, err, "Delete failed")

	// Test Get after delete
	_, err = dao.Get(testKey)
	assert.ErrorIs(t, err, ErrNotFound, "Expected ErrNotFound after delete")

	// Test Delete non-existent key
	err = dao.Delete("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound, "Expected ErrNotFound when deleting non-existent key")

	// Test List after delete
	keys, err = dao.List()
	require.NoError(t, err, "List after delete failed")
	assert.Len(t, keys, 0, "List should be empty after delete")
}

func TestSecureVaultDAO(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Generate a key
	key, err := crypto.Generate(32)
	require.NoError(t, err, "Failed to generate key")

	dao := NewSecureVaultDAO(db, key)

	// Test Get on non-existent key
	_, err = dao.Get("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound, "Expected ErrNotFound for non-existent key")

	// Test Put
	testKey := "secure_key"
	testValue := []byte("secure_value")
	err = dao.Put(testKey, testValue)
	require.NoError(t, err, "Put failed")

	// Test Get
	value, err := dao.Get(testKey)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, testValue, value, "Value mismatch")

	// Verify the value is actually encrypted in the database
	var rawValue []byte
	err = db.QueryRow("SELECT value FROM vault WHERE key = ?", testKey).Scan(&rawValue)
	require.NoError(t, err, "Failed to query raw value")
	assert.NotEqual(t, testValue, rawValue, "Value should be encrypted in the database")

	// Test with a different key (should fail to decrypt)
	wrongKey, err := crypto.Generate(32)
	require.NoError(t, err, "Failed to generate wrong key")
	wrongDAO := NewSecureVaultDAO(db, wrongKey)

	_, err = wrongDAO.Get(testKey)
	assert.Error(t, err, "Get with wrong key should fail")

	// Test List
	keys, err := dao.List()
	require.NoError(t, err, "List failed")
	assert.Contains(t, keys, testKey, "List should contain the test key")

	// Test Delete
	err = dao.Delete(testKey)
	require.NoError(t, err, "Delete failed")

	// Test Get after delete
	_, err = dao.Get(testKey)
	assert.ErrorIs(t, err, ErrNotFound, "Expected ErrNotFound after delete")
}

func TestSecureVaultDAORotateKey(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Generate original key
	originalKey, err := crypto.Generate(32)
	require.NoError(t, err, "Failed to generate original key")

	dao := NewSecureVaultDAO(db, originalKey)

	// Add some test data
	testData := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	for k, v := range testData {
		err = dao.Put(k, v)
		require.NoError(t, err, "Failed to put test data")
	}

	// Generate new key
	newKey, err := crypto.Generate(32)
	require.NoError(t, err, "Failed to generate new key")

	// Rotate the key
	err = dao.RotateKey(newKey)
	require.NoError(t, err, "Key rotation failed")

	// Verify data can be accessed with new key
	newDAO := NewSecureVaultDAO(db, newKey)
	for k, expectedValue := range testData {
		value, err := newDAO.Get(k)
		require.NoError(t, err, "Failed to get value with new key")
		assert.Equal(t, expectedValue, value, "Value mismatch after key rotation")
	}

	// Verify data cannot be accessed with old key
	oldDAO := NewSecureVaultDAO(db, originalKey)
	for k := range testData {
		_, err := oldDAO.Get(k)
		assert.Error(t, err, "Should not be able to decrypt with old key")
	}
}
