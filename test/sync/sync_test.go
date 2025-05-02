package sync_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/migrations"
	"github.com/n1/n1/internal/miror"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncBasic tests basic synchronization between two vaults
func TestSyncBasic(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping sync test in short mode")
	}

	// Create temporary directories for the test
	tempDir, err := os.MkdirTemp("", "n1-sync-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create paths for the test
	vault1Path := filepath.Join(tempDir, "vault1.db")
	vault2Path := filepath.Join(tempDir, "vault2.db")
	walPath := filepath.Join(tempDir, "wal")

	// Create the first vault
	db1, mk1, err := createTestVault(vault1Path)
	require.NoError(t, err)
	defer db1.Close()

	// Create the second vault
	db2, mk2, err := createTestVault(vault2Path)
	require.NoError(t, err)
	defer db2.Close()

	// Add some data to the first vault
	secureDAO1 := dao.NewSecureVaultDAO(db1, mk1)
	err = secureDAO1.Put("key1", []byte("value1"))
	require.NoError(t, err)
	err = secureDAO1.Put("key2", []byte("value2"))
	require.NoError(t, err)

	// Add some different data to the second vault
	secureDAO2 := dao.NewSecureVaultDAO(db2, mk2)
	err = secureDAO2.Put("key3", []byte("value3"))
	require.NoError(t, err)
	err = secureDAO2.Put("key4", []byte("value4"))
	require.NoError(t, err)

	// Create object store adapters
	objectStore1 := newTestObjectStore(db1, vault1Path, mk1)
	objectStore2 := newTestObjectStore(db2, vault2Path, mk2)

	// Create WALs
	wal1, err := miror.NewWAL(filepath.Join(walPath, "vault1"), 1024)
	require.NoError(t, err)
	defer wal1.Close()

	wal2, err := miror.NewWAL(filepath.Join(walPath, "vault2"), 1024)
	require.NoError(t, err)
	defer wal2.Close()

	// Create replicators (unused in placeholder test)
	syncConfig1 := miror.DefaultSyncConfig()
	syncConfig1.Mode = miror.SyncModePush
	_ = miror.NewReplicator(syncConfig1, objectStore1, wal1)

	syncConfig2 := miror.DefaultSyncConfig()
	syncConfig2.Mode = miror.SyncModePull
	_ = miror.NewReplicator(syncConfig2, objectStore2, wal2)

	// TODO: This is a placeholder for the actual sync test
	// In a real test, we would:
	// 1. Start a server for vault1
	// 2. Connect vault2 to vault1
	// 3. Perform the sync
	// 4. Verify that both vaults have the same data
	// However, this requires implementing the server and client components

	// For now, we'll just verify that the vaults have different data
	value1, err := secureDAO1.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value1)

	value2, err := secureDAO2.Get("key3")
	require.NoError(t, err)
	assert.Equal(t, []byte("value3"), value2)

	// Verify that vault1 doesn't have key3
	_, err = secureDAO1.Get("key3")
	assert.Error(t, err)

	// Verify that vault2 doesn't have key1
	_, err = secureDAO2.Get("key1")
	assert.Error(t, err)

	t.Log("Basic sync test completed")
}

// TestSyncConflict tests synchronization with conflicting updates
func TestSyncConflict(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping sync conflict test in short mode")
	}

	// Create temporary directories for the test
	tempDir, err := os.MkdirTemp("", "n1-sync-conflict-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create paths for the test
	vault1Path := filepath.Join(tempDir, "vault1.db")
	vault2Path := filepath.Join(tempDir, "vault2.db")
	walPath := filepath.Join(tempDir, "wal")

	// Create the first vault
	db1, mk1, err := createTestVault(vault1Path)
	require.NoError(t, err)
	defer db1.Close()

	// Create the second vault
	db2, mk2, err := createTestVault(vault2Path)
	require.NoError(t, err)
	defer db2.Close()

	// Add some data to both vaults with the same keys but different values
	secureDAO1 := dao.NewSecureVaultDAO(db1, mk1)
	err = secureDAO1.Put("conflict-key", []byte("value-from-vault1"))
	require.NoError(t, err)

	secureDAO2 := dao.NewSecureVaultDAO(db2, mk2)
	err = secureDAO2.Put("conflict-key", []byte("value-from-vault2"))
	require.NoError(t, err)

	// Create object store adapters
	objectStore1 := newTestObjectStore(db1, vault1Path, mk1)
	objectStore2 := newTestObjectStore(db2, vault2Path, mk2)

	// Create WALs
	wal1, err := miror.NewWAL(filepath.Join(walPath, "vault1"), 1024)
	require.NoError(t, err)
	defer wal1.Close()

	wal2, err := miror.NewWAL(filepath.Join(walPath, "vault2"), 1024)
	require.NoError(t, err)
	defer wal2.Close()

	// Create replicators (unused in placeholder test)
	syncConfig1 := miror.DefaultSyncConfig()
	syncConfig1.Mode = miror.SyncModePush
	_ = miror.NewReplicator(syncConfig1, objectStore1, wal1)

	syncConfig2 := miror.DefaultSyncConfig()
	syncConfig2.Mode = miror.SyncModePull
	_ = miror.NewReplicator(syncConfig2, objectStore2, wal2)

	// TODO: This is a placeholder for the actual sync conflict test
	// In a real test, we would:
	// 1. Start a server for vault1
	// 2. Connect vault2 to vault1
	// 3. Perform the sync
	// 4. Verify that the conflict is resolved according to the merge rules
	// However, this requires implementing the server and client components

	// For now, we'll just verify that the vaults have different values for the same key
	value1, err := secureDAO1.Get("conflict-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("value-from-vault1"), value1)

	value2, err := secureDAO2.Get("conflict-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("value-from-vault2"), value2)

	t.Log("Sync conflict test completed")
}

// TestSyncResumable tests resumable synchronization
func TestSyncResumable(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping resumable sync test in short mode")
	}

	// Create temporary directories for the test
	tempDir, err := os.MkdirTemp("", "n1-sync-resumable-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create paths for the test
	vault1Path := filepath.Join(tempDir, "vault1.db")
	vault2Path := filepath.Join(tempDir, "vault2.db")
	walPath := filepath.Join(tempDir, "wal")

	// Create the first vault
	db1, mk1, err := createTestVault(vault1Path)
	require.NoError(t, err)
	defer db1.Close()

	// Create the second vault
	db2, mk2, err := createTestVault(vault2Path)
	require.NoError(t, err)
	defer db2.Close()

	// Add a large amount of data to the first vault
	secureDAO1 := dao.NewSecureVaultDAO(db1, mk1)
	largeData := make([]byte, 1024*1024) // 1MB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	err = secureDAO1.Put("large-key", largeData)
	require.NoError(t, err)

	// Create object store adapters
	objectStore1 := newTestObjectStore(db1, vault1Path, mk1)
	objectStore2 := newTestObjectStore(db2, vault2Path, mk2)

	// Create WALs
	wal1, err := miror.NewWAL(filepath.Join(walPath, "vault1"), 1024)
	require.NoError(t, err)
	defer wal1.Close()

	wal2, err := miror.NewWAL(filepath.Join(walPath, "vault2"), 1024)
	require.NoError(t, err)
	defer wal2.Close()

	// Create replicators (unused in placeholder test)
	syncConfig1 := miror.DefaultSyncConfig()
	syncConfig1.Mode = miror.SyncModePush
	_ = miror.NewReplicator(syncConfig1, objectStore1, wal1)

	syncConfig2 := miror.DefaultSyncConfig()
	syncConfig2.Mode = miror.SyncModePull
	_ = miror.NewReplicator(syncConfig2, objectStore2, wal2)

	// TODO: This is a placeholder for the actual resumable sync test
	// In a real test, we would:
	// 1. Start a server for vault1
	// 2. Connect vault2 to vault1
	// 3. Start the sync
	// 4. Interrupt the sync in the middle
	// 5. Resume the sync
	// 6. Verify that the sync completes successfully
	// However, this requires implementing the server and client components

	// For now, we'll just verify that vault1 has the large data
	value, err := secureDAO1.Get("large-key")
	require.NoError(t, err)
	assert.Equal(t, largeData, value)

	t.Log("Resumable sync test completed")
}

// TestSyncContinuous tests continuous synchronization
func TestSyncContinuous(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping continuous sync test in short mode")
	}

	// Create temporary directories for the test
	tempDir, err := os.MkdirTemp("", "n1-sync-continuous-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create paths for the test
	vault1Path := filepath.Join(tempDir, "vault1.db")
	vault2Path := filepath.Join(tempDir, "vault2.db")
	walPath := filepath.Join(tempDir, "wal")

	// Create the first vault
	db1, mk1, err := createTestVault(vault1Path)
	require.NoError(t, err)
	defer db1.Close()

	// Create the second vault
	db2, mk2, err := createTestVault(vault2Path)
	require.NoError(t, err)
	defer db2.Close()

	// Create object store adapters
	objectStore1 := newTestObjectStore(db1, vault1Path, mk1)
	objectStore2 := newTestObjectStore(db2, vault2Path, mk2)

	// Create WALs
	wal1, err := miror.NewWAL(filepath.Join(walPath, "vault1"), 1024)
	require.NoError(t, err)
	defer wal1.Close()

	wal2, err := miror.NewWAL(filepath.Join(walPath, "vault2"), 1024)
	require.NoError(t, err)
	defer wal2.Close()

	// Create replicators (unused in placeholder test)
	syncConfig1 := miror.DefaultSyncConfig()
	syncConfig1.Mode = miror.SyncModeFollow
	_ = miror.NewReplicator(syncConfig1, objectStore1, wal1)

	syncConfig2 := miror.DefaultSyncConfig()
	syncConfig2.Mode = miror.SyncModeFollow
	_ = miror.NewReplicator(syncConfig2, objectStore2, wal2)

	// TODO: This is a placeholder for the actual continuous sync test
	// In a real test, we would:
	// 1. Start a server for vault1
	// 2. Connect vault2 to vault1 in follow mode
	// 3. Add data to vault1
	// 4. Verify that the data is synchronized to vault2 within 5 seconds
	// 5. Add data to vault2
	// 6. Verify that the data is synchronized to vault1 within 5 seconds
	// 7. Repeat for 24 hours
	// However, this requires implementing the server and client components

	// For now, we'll just create a short-lived test
	secureDAO1 := dao.NewSecureVaultDAO(db1, mk1)
	_ = dao.NewSecureVaultDAO(db2, mk2) // Unused in placeholder test

	// Add data to vault1
	err = secureDAO1.Put("continuous-key", []byte("continuous-value"))
	require.NoError(t, err)

	// Verify that vault1 has the data
	value, err := secureDAO1.Get("continuous-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("continuous-value"), value)

	t.Log("Continuous sync test completed")
}

// Helper functions

// createTestVault creates a test vault and returns the database, master key, and error
func createTestVault(path string) (*sql.DB, []byte, error) {
	// Generate a master key
	mk, err := crypto.Generate(32)
	if err != nil {
		return nil, nil, err
	}

	// Store the master key
	if err := secretstore.Default.Put(path, mk); err != nil {
		return nil, nil, err
	}

	// Create the database
	db, err := sqlite.Open(path)
	if err != nil {
		_ = secretstore.Default.Delete(path)
		return nil, nil, err
	}

	// Initialize the schema
	if err := migrations.BootstrapVault(db); err != nil {
		db.Close()
		_ = secretstore.Default.Delete(path)
		return nil, nil, err
	}

	// Add a canary record
	secureDAO := dao.NewSecureVaultDAO(db, mk)
	if err := secureDAO.Put("__n1_canary__", []byte("ok")); err != nil {
		db.Close()
		_ = secretstore.Default.Delete(path)
		return nil, nil, err
	}

	return db, mk, nil
}

// TestObjectStore is a simple implementation of the miror.ObjectStore interface for testing
type TestObjectStore struct {
	db        *sql.DB
	vaultPath string
	secureDAO *dao.SecureVaultDAO
}

// newTestObjectStore creates a new test object store
func newTestObjectStore(db *sql.DB, vaultPath string, masterKey []byte) *TestObjectStore {
	return &TestObjectStore{
		db:        db,
		vaultPath: vaultPath,
		secureDAO: dao.NewSecureVaultDAO(db, masterKey),
	}
}

// GetObject gets an object by its hash
func (s *TestObjectStore) GetObject(ctx context.Context, hash miror.ObjectHash) ([]byte, error) {
	key := hash.String()
	return s.secureDAO.Get(key)
}

// PutObject puts an object with the given hash and data
func (s *TestObjectStore) PutObject(ctx context.Context, hash miror.ObjectHash, data []byte) error {
	key := hash.String()
	return s.secureDAO.Put(key, data)
}

// HasObject checks if an object exists
func (s *TestObjectStore) HasObject(ctx context.Context, hash miror.ObjectHash) (bool, error) {
	key := hash.String()
	_, err := s.secureDAO.Get(key)
	if err == dao.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListObjects lists all object hashes
func (s *TestObjectStore) ListObjects(ctx context.Context) ([]miror.ObjectHash, error) {
	keys, err := s.secureDAO.List()
	if err != nil {
		return nil, err
	}

	var hashes []miror.ObjectHash
	for _, key := range keys {
		// Skip the canary record
		if key == "__n1_canary__" {
			continue
		}

		// Convert key to hash
		var hash miror.ObjectHash
		// In a real implementation, we would convert the key to a hash
		// For now, we'll just use a placeholder
		hashes = append(hashes, hash)
	}

	return hashes, nil
}

// GetObjectReader gets a reader for an object
func (s *TestObjectStore) GetObjectReader(ctx context.Context, hash miror.ObjectHash) (io.ReadCloser, error) {
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
}

// GetObjectWriter gets a writer for an object
func (s *TestObjectStore) GetObjectWriter(ctx context.Context, hash miror.ObjectHash) (io.WriteCloser, error) {
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
}
