// Command mirord is the daemon process for n1 synchronization.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/miror"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const (
	// DefaultConfigPath is the default path for the mirord configuration file.
	DefaultConfigPath = "~/.config/n1/mirord.yaml"
	// DefaultWALPath is the default path for the mirord WAL directory.
	DefaultWALPath = "~/.local/share/n1/mirord/wal"
	// DefaultPIDFile is the default path for the mirord PID file.
	DefaultPIDFile = "~/.local/share/n1/mirord/mirord.pid"
)

// Config represents the configuration for the mirord daemon.
type Config struct {
	// VaultPath is the path to the vault file.
	VaultPath string
	// WALPath is the path to the WAL directory.
	WALPath string
	// PIDFile is the path to the PID file.
	PIDFile string
	// LogLevel is the logging level.
	LogLevel string
	// ListenAddresses are the addresses to listen on.
	ListenAddresses []string
	// Peers are the known peers.
	Peers []string
	// DiscoveryEnabled indicates whether mDNS discovery is enabled.
	DiscoveryEnabled bool
	// SyncInterval is the interval for automatic synchronization.
	SyncInterval time.Duration
	// TransportConfig is the transport configuration.
	TransportConfig miror.TransportConfig
	// SyncConfig is the synchronization configuration.
	SyncConfig miror.SyncConfig
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		VaultPath:        "", // Must be provided via flag
		WALPath:          expandPath(DefaultWALPath),
		PIDFile:          expandPath(DefaultPIDFile),
		LogLevel:         "info",
		ListenAddresses:  []string{":7001"}, // Default to one standard port
		Peers:            []string{},
		DiscoveryEnabled: true,
		SyncInterval:     5 * time.Minute,
		TransportConfig:  miror.DefaultTransportConfig(),
		SyncConfig:       miror.DefaultSyncConfig(),
	}
}

// expandPath expands the ~ in a path to the user's home directory.
func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path // Silently ignore error, return original path
	}

	return filepath.Join(home, path[1:])
}

// writePIDFile writes the current process ID to the PID file.
func writePIDFile(path string) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for PID file: %w", err)
	}

	// Write the PID
	pid := os.Getpid()
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0600); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

// removePIDFile removes the PID file.
func removePIDFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// --- ObjectStoreAdapter (Implements real content hashing) ---

// --- ObjectStoreAdapter (Implements real content hashing) ---

// ObjectStoreAdapter adapts the vault DAO to the miror.ObjectStore interface
type ObjectStoreAdapter struct {
	db        *sql.DB
	vaultPath string
	secureDAO *dao.SecureVaultDAO // Used for Put/Get operations needing encryption/decryption
	// hashToKey maps object hashes (as strings) to their user-defined keys in the vault
	hashToKey map[string]string
	// keyToHash maps user-defined keys to their content hashes
	keyToHash map[string]miror.ObjectHash
}

// NewObjectStoreAdapter creates a new adapter for the vault
func NewObjectStoreAdapter(db *sql.DB, vaultPath string, masterKey []byte) *ObjectStoreAdapter {
	adapter := &ObjectStoreAdapter{
		db:        db,
		vaultPath: vaultPath,
		secureDAO: dao.NewSecureVaultDAO(db, masterKey), // Initialize Secure DAO
		hashToKey: make(map[string]string),
		keyToHash: make(map[string]miror.ObjectHash),
	}

	// Initialize the hash mappings upon creation
	adapter.initHashMappings()

	return adapter
}

// computeObjectHash computes the SHA-256 hash of the ENCRYPTED value blob.
// This should be a method of the adapter if it needs adapter state, but it doesn't.
// Let's make it an unexported helper function within this file for clarity.
func computeObjectHash(encryptedValue []byte) miror.ObjectHash {
	var hash miror.ObjectHash
	h := sha256.Sum256(encryptedValue)
	copy(hash[:], h[:])
	return hash
}

// initHashMappings initializes the hash-to-key and key-to-hash mappings
func (a *ObjectStoreAdapter) initHashMappings() {
	log.Debug().Msg("ObjectStoreAdapter: Initializing hash mappings...")

	rawDAO := dao.NewVaultDAO(a.db) // Use raw DAO to list keys and get encrypted blobs
	keys, err := rawDAO.List()
	if err != nil {
		log.Error().Err(err).Msg("ObjectStoreAdapter.initHashMappings: Failed to list keys from raw DAO")
		return
	}
	log.Debug().Int("key_count", len(keys)).Msg("ObjectStoreAdapter.initHashMappings: Listed keys")

	// Clear existing maps before rebuilding
	a.hashToKey = make(map[string]string)
	a.keyToHash = make(map[string]miror.ObjectHash)

	processedCount := 0
	for _, key := range keys {
		// Skip the canary record - its hash isn't relevant for sync
		if key == "__n1_canary__" || strings.HasPrefix(key, miror.ObjectHash{}.String()) { // Also skip keys that *are* hashes from previous syncs
			log.Debug().Str("key", key).Msg("ObjectStoreAdapter.initHashMappings: Skipping internal/hash key")
			continue
		}

		record, err := rawDAO.Get(key) // Get the raw record with encrypted blob
		if err != nil {
			log.Error().Err(err).Str("key", key).Msg("ObjectStoreAdapter.initHashMappings: Failed to get raw vault record")
			continue // Skip this key if raw fetch fails
		}
		encryptedValue := record.Value

		// Compute the hash of the *encrypted* value
		hash := computeObjectHash(encryptedValue) // Use the helper function
		hashStr := hash.String()

		// Store the mappings: hash -> key AND key -> hash
		a.hashToKey[hashStr] = key
		a.keyToHash[key] = hash
		log.Debug().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.initHashMappings: Mapped key to hash")
		processedCount++
	}
	log.Debug().Int("processed_count", processedCount).Int("map_size", len(a.keyToHash)).Msg("ObjectStoreAdapter.initHashMappings: Finished processing keys")
}

// GetObject gets an object's *decrypted* data by its content hash (hash of encrypted blob).
func (a *ObjectStoreAdapter) GetObject(ctx context.Context, hash miror.ObjectHash) ([]byte, error) {
	hashStr := hash.String() // Define hashStr here
	log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.GetObject called")

	// Look up the key associated with this hash
	// This key could be a user-defined key OR the hash itself if stored via PutObject
	key, exists := a.hashToKey[hashStr]
	if !exists {
		// If the hash isn't in the map, the object doesn't exist (or wasn't mapped)
		log.Warn().Str("hash", hashStr).Msg("ObjectStoreAdapter.GetObject: Hash not found in hashToKey map")

		// As a fallback, check if the key *is* the hash (object stored by PutObject)
		log.Debug().Str("hash_as_key", hashStr).Msg("ObjectStoreAdapter.GetObject: Checking if hash exists as key directly...")
		decryptedValue, err := a.secureDAO.Get(hashStr) // Try getting directly using hash as key
		if err == nil {
			log.Info().Str("hash_as_key", hashStr).Msg("ObjectStoreAdapter.GetObject: Found object directly using hash as key")

			// Verify the hash of the *retrieved and re-encrypted* data still matches
			// This requires getting the raw encrypted blob again.
			rawDAO := dao.NewVaultDAO(a.db)
			record, rawErr := rawDAO.Get(hashStr)
			if rawErr != nil {
				log.Error().Err(rawErr).Str("key", hashStr).Msg("ObjectStoreAdapter.GetObject: Failed to get raw record for hash-key")
				return nil, fmt.Errorf("failed to get raw record for hash-key %s: %w", hashStr, rawErr)
			}
			recomputedHash := computeObjectHash(record.Value)
			if recomputedHash.String() != hashStr {
				log.Error().Str("key", hashStr).Str("expected_hash", hashStr).Str("recomputed_hash", recomputedHash.String()).Msg("ObjectStoreAdapter.GetObject: Hash mismatch for object stored by hash!")
				return nil, fmt.Errorf("hash mismatch for object stored by hash %s", hashStr)
			}

			return decryptedValue, nil // Return the decrypted value
		}
		if !errors.Is(err, dao.ErrNotFound) {
			log.Error().Err(err).Str("hash_as_key", hashStr).Msg("ObjectStoreAdapter.GetObject: Error trying to get object directly using hash as key")
		}

		return nil, dao.ErrNotFound // Definitely not found
	}
	log.Debug().Str("hash", hashStr).Str("key", key).Msg("ObjectStoreAdapter.GetObject: Found user-defined key for hash")

	// Get the raw encrypted blob to verify the hash *before* decrypting
	rawDAO := dao.NewVaultDAO(a.db)
	record, err := rawDAO.Get(key)
	if err != nil {
		log.Error().Err(err).Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.GetObject: Failed to get raw vault record for key")
		// This indicates an inconsistency if the key was in the map but not DB
		delete(a.keyToHash, key) // Clean up inconsistent map entry
		delete(a.hashToKey, hashStr)
		return nil, fmt.Errorf("internal inconsistency: key %s for hash %s not found in DB: %w", key, hashStr, err)
	}
	encryptedValue := record.Value

	// Verify the hash of the stored encrypted blob matches the requested hash
	computedHash := computeObjectHash(encryptedValue)
	if computedHash.String() != hashStr {
		log.Error().Str("key", key).Str("expected_hash", hashStr).Str("computed_hash", computedHash.String()).Msg("ObjectStoreAdapter.GetObject: Hash mismatch!")
		// Hash mismatch means the map is stale or data is corrupt
		delete(a.keyToHash, key) // Clean up inconsistent map entry
		delete(a.hashToKey, hashStr)
		return nil, fmt.Errorf("hash mismatch for key %s: expected %s, got %s", key, hashStr, computedHash.String())
	}
	log.Debug().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.GetObject: Hash verified")

	// Now, get and decrypt the value using SecureDAO
	decryptedValue, err := a.secureDAO.Get(key)
	if err != nil {
		log.Error().Err(err).Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.GetObject: Failed to get/decrypt value via secureDAO")
		return nil, fmt.Errorf("failed to decrypt value for key %s: %w", key, err)
	}
	log.Debug().Str("key", key).Str("hash", hashStr).Int("decrypted_size", len(decryptedValue)).Msg("ObjectStoreAdapter.GetObject: Value decrypted successfully")

	return decryptedValue, nil
}

// PutObject stores the *decrypted* data, associating it with the provided content hash.
// The hash is used as the key in the underlying vault for content-addressable storage during sync.
func (a *ObjectStoreAdapter) PutObject(ctx context.Context, hash miror.ObjectHash, data []byte) error {
	hashStr := hash.String() // Define hashStr here
	log.Debug().Str("hash", hashStr).Int("data_size", len(data)).Msg("ObjectStoreAdapter.PutObject called")

	// Get the master key (needed for encryption by SecureDAO)
	masterKey, err := secretstore.Default.Get(a.vaultPath)
	if err != nil {
		log.Error().Err(err).Str("vaultPath", a.vaultPath).Msg("ObjectStoreAdapter.PutObject: Failed to get master key")
		return fmt.Errorf("failed to get master key: %w", err)
	}

	// Encrypt the data temporarily to compute the hash *of the encrypted blob*
	log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Encrypting data for hash verification...")
	encryptedDataForHash, err := crypto.EncryptBlob(masterKey, data)
	if err != nil {
		log.Error().Err(err).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Failed to encrypt data for hash verification")
		return fmt.Errorf("failed to encrypt data for hash verification: %w", err)
	}

	// Compute the hash of the encrypted data
	computedHash := computeObjectHash(encryptedDataForHash) // Use helper
	log.Debug().Str("provided_hash", hashStr).Str("computed_hash", computedHash.String()).Msg("ObjectStoreAdapter.PutObject: Computed hash of encrypted data")

	// Verify the hash of the *potential* encrypted blob matches the provided hash
	if !bytes.Equal(computedHash[:], hash[:]) {
		log.Error().Str("expected_hash", hashStr).Str("computed_hash", computedHash.String()).Msg("ObjectStoreAdapter.PutObject: Hash mismatch!")
		return fmt.Errorf("hash mismatch: expected %s, got %s", hashStr, computedHash.String())
	}

	// Use the verified hash string as the key for storage
	key := hashStr
	log.Debug().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Using hash as key for storage")

	// Store the original *decrypted* data using the SecureDAO.
	// SecureDAO will handle encrypting it with the master key before writing to the DB.
	log.Debug().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Calling secureDAO.Put...")
	err = a.secureDAO.Put(key, data)
	if err != nil {
		log.Error().Err(err).Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: secureDAO.Put failed")
		return fmt.Errorf("failed to store object with key %s: %w", key, err)
	}

	// Update the internal maps *after* successful storage
	a.hashToKey[hashStr] = key // Map hash back to itself as the key
	a.keyToHash[key] = hash    // Map the key (which is the hash) to the hash
	log.Debug().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Updated internal hash/key maps")

	log.Info().Str("key", key).Str("hash", hashStr).Msg("ObjectStoreAdapter.PutObject: Object stored successfully")
	return nil
}

// HasObject checks if an object with the given content hash exists.
func (a *ObjectStoreAdapter) HasObject(ctx context.Context, hash miror.ObjectHash) (bool, error) {
	hashStr := hash.String() // Define hashStr here
	log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject called")

	// Check if the hash exists in our map (most common case)
	if _, exists := a.hashToKey[hashStr]; exists {
		log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject: Found hash in map")
		// Optional: Add a DB check here for extra safety, but might impact performance.
		// rawDAO := dao.NewVaultDAO(a.db)
		// _, err := rawDAO.Get(a.hashToKey[hashStr]) // Check if the mapped key exists
		// if err != nil { ... handle inconsistency ... }
		return true, nil
	}

	// If not in map, check if the object was stored directly using its hash as the key
	log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject: Hash not in map, checking DB directly with hash as key...")
	rawDAO := dao.NewVaultDAO(a.db)
	_, err := rawDAO.Get(hashStr)
	if err == nil {
		log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject: Found object directly in DB using hash as key")
		// If found directly, update the map for future lookups
		a.hashToKey[hashStr] = hashStr
		a.keyToHash[hashStr] = hash
		return true, nil
	}
	if errors.Is(err, dao.ErrNotFound) {
		log.Debug().Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject: Object not found directly in DB")
		return false, nil // Not found
	}

	// Other error occurred during DB lookup
	log.Error().Err(err).Str("hash", hashStr).Msg("ObjectStoreAdapter.HasObject: Error checking DB directly")
	return false, fmt.Errorf("failed to check object %s existence in DB: %w", hashStr, err)
}

// ListObjects lists all object hashes currently known to the adapter.
func (a *ObjectStoreAdapter) ListObjects(ctx context.Context) ([]miror.ObjectHash, error) {
	log.Debug().Msg("ObjectStoreAdapter.ListObjects called")
	var hashes []miror.ObjectHash

	// Rebuild map on list to ensure consistency
	log.Debug().Msg("ObjectStoreAdapter.ListObjects: Re-initializing hash maps for consistency...")
	a.initHashMappings() // Re-run the mapping initialization
	log.Debug().Int("hash_count", len(a.keyToHash)).Msg("ObjectStoreAdapter.ListObjects: Hash maps re-initialized")

	// Add hashes from the keyToHash map (user-defined keys)
	for _, hash := range a.keyToHash {
		hashes = append(hashes, hash)
	}

	// Additionally, list hashes that were stored directly (where key == hash)
	// This requires listing all keys and checking which ones are valid hashes
	rawDAO := dao.NewVaultDAO(a.db)
	allKeys, err := rawDAO.List()
	if err != nil {
		log.Error().Err(err).Msg("ObjectStoreAdapter.ListObjects: Failed to list all keys from rawDAO")
		return nil, fmt.Errorf("failed to list all keys for hash check: %w", err)
	}

	keysAlreadyMapped := make(map[string]bool)
	for k := range a.keyToHash {
		keysAlreadyMapped[k] = true
	}

	for _, key := range allKeys {
		// Skip keys already mapped and internal keys
		if keysAlreadyMapped[key] || key == "__n1_canary__" {
			continue
		}
		// Check if the key looks like a SHA256 hash (64 hex chars)
		if len(key) == 64 {
			isHex := true
			for _, r := range key {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
					isHex = false
					break
				}
			}
			if isHex {
				// Attempt to decode the hash - if successful, add it
				var potentialHash miror.ObjectHash
				_, err := hex.Decode(potentialHash[:], []byte(key))
				if err == nil {
					log.Debug().Str("hash_key", key).Msg("ObjectStoreAdapter.ListObjects: Adding hash found directly as key")
					hashes = append(hashes, potentialHash)
				}
			}
		}
	}

	log.Debug().Int("hash_count_returned", len(hashes)).Msg("ObjectStoreAdapter.ListObjects returning hashes")

	// Deduplicate (although the logic above should prevent duplicates if maps are correct)
	uniqueHashes := make([]miror.ObjectHash, 0, len(hashes))
	seenHashes := make(map[string]struct{})
	for _, h := range hashes {
		hStr := h.String()
		if _, seen := seenHashes[hStr]; !seen {
			uniqueHashes = append(uniqueHashes, h)
			seenHashes[hStr] = struct{}{}
		}
	}

	return uniqueHashes, nil
}

// GetObjectReader gets a reader for an object's decrypted data.
func (a *ObjectStoreAdapter) GetObjectReader(ctx context.Context, hash miror.ObjectHash) (io.ReadCloser, error) {
	log.Debug().Str("hash", hash.String()).Msg("ObjectStoreAdapter.GetObjectReader called")
	data, err := a.GetObject(ctx, hash) // Calls the already logged GetObject
	if err != nil {
		return nil, err // Error already logged in GetObject
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetObjectWriter gets a writer for an object's decrypted data.
func (a *ObjectStoreAdapter) GetObjectWriter(ctx context.Context, hash miror.ObjectHash) (io.WriteCloser, error) {
	log.Debug().Str("hash", hash.String()).Msg("ObjectStoreAdapter.GetObjectWriter called")
	buf := &bytes.Buffer{}
	return &objectWriter{
		buffer:      buf,
		hash:        hash,
		objectStore: a, // Pass the adapter itself
		ctx:         ctx,
	}, nil
}

// --- objectWriter remains the same, but ensure it calls the adapter's PutObject ---

// objectWriter is a WriteCloser that writes to a buffer and then to the object store when closed
type objectWriter struct {
	buffer      *bytes.Buffer
	hash        miror.ObjectHash // Expected hash of the *encrypted* blob
	objectStore *ObjectStoreAdapter
	ctx         context.Context
}

func (w *objectWriter) Write(p []byte) (n int, err error) {
	return w.buffer.Write(p)
}

func (w *objectWriter) Close() error {
	// This Close method now just calls PutObject on the adapter.
	// PutObject handles the hash verification and storage.
	data := w.buffer.Bytes() // This is the *decrypted* data written by the caller
	log := log.Logger.With().Str("hash_expected", w.hash.String()).Int("data_size", len(data)).Logger()
	log.Debug().Msg("objectWriter.Close: Calling objectStore.PutObject")

	// Pass the expected hash and the decrypted data to PutObject
	err := w.objectStore.PutObject(w.ctx, w.hash, data)
	if err != nil {
		log.Error().Err(err).Msg("objectWriter.Close: objectStore.PutObject failed")
		return err
	}

	log.Debug().Msg("objectWriter.Close: objectStore.PutObject succeeded")
	return nil
}

// --- End ObjectStoreAdapter ---

// runDaemon runs the mirord daemon with the given configuration.
func runDaemon(config Config) error {
	// Set up logging
	level, err := zerolog.ParseLevel(config.LogLevel)
	if err != nil {
		log.SetLevel(zerolog.InfoLevel) // Default to info on parse error
		log.Error().Err(err).Str("level", config.LogLevel).Msg("Invalid log level provided, defaulting to info")
		// Return error instead of just logging? Depends on desired strictness.
		// return fmt.Errorf("invalid log level: %w", err)
	} else {
		log.SetLevel(level)
	}
	log.Info().Str("logLevel", level.String()).Msg("Mirord log level set") // Log the actual level

	// Validate config
	if config.VaultPath == "" {
		return errors.New("vault path must be provided")
	}
	config.VaultPath = expandPath(config.VaultPath) // Ensure vault path is expanded
	log.Info().Str("vaultPath", config.VaultPath).Msg("Using vault path")

	if len(config.ListenAddresses) == 0 {
		return errors.New("at least one listen address must be provided")
	}

	// Write PID file
	config.PIDFile = expandPath(config.PIDFile)
	if err := writePIDFile(config.PIDFile); err != nil {
		// Log error but maybe continue? Or return err?
		log.Error().Err(err).Str("path", config.PIDFile).Msg("Failed to write PID file")
		// return err // Might be too strict depending on use case
	} else {
		log.Info().Str("pidPath", config.PIDFile).Msg("PID file written")
	}
	defer func() {
		if err := removePIDFile(config.PIDFile); err != nil {
			log.Error().Err(err).Str("path", config.PIDFile).Msg("Failed to remove PID file on exit")
		} else {
			log.Info().Str("path", config.PIDFile).Msg("Removed PID file")
		}
	}()

	// Get master key
	log.Info().Str("vaultPath", config.VaultPath).Msg("Attempting to retrieve master key...")
	mk, err := secretstore.Default.Get(config.VaultPath)
	if err != nil {
		log.Error().Err(err).Str("vaultPath", config.VaultPath).Msg("Failed to get master key from secret store")
		return fmt.Errorf("failed to get key from secret store for vault %s: %w", config.VaultPath, err)
	}
	log.Info().Msg("Master key retrieved successfully")

	// Open DB
	log.Info().Str("vaultPath", config.VaultPath).Msg("Attempting to open database...")
	db, err := sqlite.Open(config.VaultPath)
	if err != nil {
		log.Error().Err(err).Str("vaultPath", config.VaultPath).Msg("Failed to open database file")
		return fmt.Errorf("failed to open database file '%s': %w", config.VaultPath, err)
	}
	defer func() {
		log.Info().Msg("Closing database connection...")
		if err := db.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing database")
		} else {
			log.Info().Msg("Database connection closed")
		}
	}()
	log.Info().Msg("Database opened successfully")

	// Create ObjectStore and WAL
	log.Info().Msg("Creating Object Store Adapter...")
	var objectStore *ObjectStoreAdapter // Declare variable
	func() {                            // Use anonymous func for panic recovery during init
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic_value", r).Bytes("stack", debug.Stack()).Msg("PANIC recovered during ObjectStoreAdapter creation")
				// Propagate panic as error to stop daemon startup
				err = fmt.Errorf("panic during object store creation: %v", r)
			}
		}()
		objectStore = NewObjectStoreAdapter(db, config.VaultPath, mk) // Assign inside func
	}()
	if err != nil { // Check if panic occurred during creation
		return err
	}
	if objectStore == nil { // Should not happen if panic doesn't occur, but belt-and-suspenders
		return fmt.Errorf("object store adapter is nil after creation without panic")
	}
	log.Info().Msg("Object Store Adapter created") // Log success *after* creation

	config.WALPath = expandPath(config.WALPath)
	log.Info().Str("walPath", config.WALPath).Msg("Creating WAL...")
	// Using a new variable 'walErr' for clarity here.
	wal, walErr := miror.NewWAL(config.WALPath, 1024*1024) // Assign WAL correctly
	if walErr != nil {
		log.Error().Err(walErr).Str("walPath", config.WALPath).Msg("Failed to create WAL")
		return fmt.Errorf("failed to create WAL at %s: %w", config.WALPath, walErr)
	}
	defer func() {
		log.Info().Msg("Closing WAL...")
		if err := wal.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing WAL")
		} else {
			log.Info().Msg("WAL closed")
		}
	}()
	log.Info().Msg("WAL created successfully")

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalCh
		log.Info().Str("signal", sig.String()).Msg("Received signal, initiating shutdown...")
		cancel()
	}()

	// Start listener(s)
	listeners := make([]net.Listener, 0, len(config.ListenAddresses))
	for _, addr := range config.ListenAddresses {
		log.Info().Str("address", addr).Msg("Attempting to listen...")
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Error().Err(err).Str("address", addr).Msg("Failed to listen")
			// Clean up already opened listeners before returning
			for _, l := range listeners {
				l.Close()
			}
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		actualAddr := listener.Addr().String() // Get the actual listening address
		log.Info().Str("address", actualAddr).Msg("Successfully listening for connections")
		listeners = append(listeners, listener)

		// Start accept loop for this listener
		go func(l net.Listener, w miror.WAL) {
			addrStr := l.Addr().String() // Capture address string for logging
			log.Info().Str("address", addrStr).Msg("Starting accept loop...")
			for {
				conn, err := l.Accept()
				if err != nil {
					// Check if the error is due to listener being closed gracefully
					select {
					case <-ctx.Done():
						log.Info().Str("address", addrStr).Msg("Accept loop stopped: context cancelled.")
						return // Normal exit
					default:
						// Check for specific network errors that might indicate closure vs other issues
						if errors.Is(err, net.ErrClosed) {
							log.Warn().Str("address", addrStr).Msg("Accept loop stopped: Listener closed.")
							return // Exit loop if listener is closed
						}
						log.Error().Err(err).Str("address", addrStr).Msg("Failed to accept connection")
						// Potentially add a small delay before retrying to prevent tight loops on persistent errors
						time.Sleep(100 * time.Millisecond)
						continue
					}
				}
				// Log both remote and local address for clarity
				log.Info().Str("remote_addr", conn.RemoteAddr().String()).Str("local_addr", conn.LocalAddr().String()).Msg("Accepted new connection")
				// Handle connection in a new goroutine
				go handleConnection(ctx, conn, objectStore, wal, config)
			}
		}(listener, wal)
	}

	log.Info().Msg("Mirord daemon successfully started and running")

	// Wait for context cancellation (shutdown signal)
	<-ctx.Done()

	// Close listeners first to stop accepting new connections
	log.Info().Msg("Shutdown initiated: Closing listeners...")
	for _, l := range listeners {
		addrStr := l.Addr().String()
		if err := l.Close(); err != nil {
			log.Error().Err(err).Str("address", addrStr).Msg("Error closing listener")
		} else {
			log.Info().Str("address", addrStr).Msg("Listener closed successfully")
		}
	}
	log.Info().Msg("Listeners closed")

	// TODO: Implement graceful shutdown of active connections if needed

	log.Info().Msg("Mirord daemon stopped")
	return nil
}

// --- START DEBUG LOGGING Variables ---
var connectionCounter int // Simple counter for concurrent connections (not thread-safe, just for debug)
// --- END DEBUG LOGGING Variables ---

// handleConnection handles an incoming synchronization connection.
// Server always sends OFFER first.
func handleConnection(ctx context.Context, conn net.Conn, objectStore miror.ObjectStore, wal miror.WAL, config Config) {
	connectionCounter++ // Increment counter
	connNum := connectionCounter
	remoteAddr := conn.RemoteAddr().String()
	localAddr := conn.LocalAddr().String()

	// --- ADDED LOGGING ---
	// Log entry *before* creating the logger, in case logger creation fails
	fmt.Printf("[%s] handleConnection: Entered for conn_num %d from %s\n", time.Now().Format(time.RFC3339), connNum, remoteAddr)

	// Create logger specific to this connection
	log := log.Logger.With().
		Str("remote_addr", remoteAddr).
		Str("local_addr", localAddr).
		Int("conn_num", connNum).
		Logger()
	// --- ADDED LOGGING ---
	log.Debug().Msg("Connection-specific logger created")

	log.Info().Msg("Handling new connection") // Log start

	// Implement recover to catch panics within this goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic_value", r).Bytes("stack", debug.Stack()).Msg("PANIC recovered in handleConnection")
		}
		// Ensure connection is closed on exit, regardless of reason
		log.Info().Msg("Closing connection")
		conn.Close()
	}()

	// Wrap connection in Transport
	log.Debug().Msg("Attempting to create TCP transport for connection...")
	transport, err := miror.NewTCPTransport("", config.TransportConfig) // Peer address is empty for server-side
	if err != nil {
		log.Error().Err(err).Msg("Failed to create TCP transport for incoming connection")
		return // Exit handler
	}
	log.Debug().Msg("TCP transport created successfully")
	transport.SetConnection(conn) // Assign the accepted connection
	// Defer transport.Close() - We close conn directly in the main defer now

	// Create a temporary session ID for logging/WAL (should ideally come from client HELLO later)
	var sessionID miror.SessionID
	// Simple placeholder ID for M1 logging
	copy(sessionID[:], fmt.Sprintf("server-conn-%d", connNum))
	log = log.With().Str("session_id", sessionID.String()).Logger() // Add session ID to logger context

	// --- Server Sends Offer First ---
	log.Info().Msg("Preparing initial OFFER...")

	// Check context before long operation
	if err := ctx.Err(); err != nil {
		log.Warn().Err(err).Msg("Context cancelled before listing objects")
		return
	}

	// --- ADDED LOGGING ---
	log.Debug().Msg("Attempting to list objects via objectStore.ListObjects...")
	serverHashes, err := objectStore.ListObjects(ctx)
	if err != nil {
		// --- ADDED LOGGING ---
		log.Error().Err(err).Msg(">>> CRITICAL FAILURE: objectStore.ListObjects failed!")
		// TODO: Send ERROR message to client?
		// transport.Send(ctx, miror.MessageTypeError, []byte("Failed to list objects"))
		return // Exit before sending anything
	}
	// --- ADDED LOGGING ---
	log.Debug().Int("count", len(serverHashes)).Msg("objectStore.ListObjects successful.")

	log.Debug().Msg("Encoding initial OFFER...")
	offerBody, err := miror.EncodeOfferMessage(serverHashes)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode initial OFFER")
		return
	}
	log.Debug().Int("offer_body_size", len(offerBody)).Msg("OFFER encoded.")

	log.Info().Msg("Attempting to send initial OFFER message...")
	// Check context before network operation
	if err := ctx.Err(); err != nil {
		log.Warn().Err(err).Msg("Context cancelled before sending OFFER")
		return
	}
	if err := transport.Send(ctx, miror.MessageTypeOffer, offerBody); err != nil {
		log.Error().Err(err).Msg("Failed to send initial OFFER")
		return
	}
	log.Info().Msg("Successfully sent initial OFFER")

	// --- Wait for Client Response ---
	log.Info().Msg("Waiting for client response (ACCEPT, OFFER, or COMPLETE)")
	// Check context before blocking receive
	if err := ctx.Err(); err != nil {
		log.Warn().Err(err).Msg("Context cancelled before receiving client response")
		return
	}
	msgType, clientRespBody, err := transport.Receive(ctx)
	if err != nil {
		// Don't log EOF as error if context was cancelled
		if errors.Is(err, io.EOF) && ctx.Err() != nil {
			log.Info().Msg("Connection closed by client or context cancelled while waiting for response")
		} else if errors.Is(err, io.EOF) {
			log.Warn().Msg("Connection closed by client unexpectedly (EOF received)")
		} else {
			log.Error().Err(err).Msg("Failed to receive client response")
		}
		return // Exit handler in case of EOF or error
	}
	log.Info().Uint8("msg_type", msgType).Int("body_size", len(clientRespBody)).Msg("Received client response.")

	// ... rest of the switch statement remains the same ...

	switch msgType {
	case miror.MessageTypeAccept:
		log.Info().Msg("Processing ACCEPT from client")
		hashesToSend, err := miror.DecodeAcceptMessage(clientRespBody)
		if err != nil {
			log.Error().Err(err).Msg("Failed to decode client ACCEPT")
			// TODO: Send ERROR message?
			return
		}
		log.Info().Int("count", len(hashesToSend)).Msg("Client accepted objects")

		if err := sendObjects(ctx, log, transport, objectStore, wal, sessionID, hashesToSend); err != nil {
			log.Error().Err(err).Msg("Failed during server push (sending objects)")
			// Error already logged in sendObjects, just return
			return
		}
		// After sending objects, server expects COMPLETE from client
		log.Info().Msg("Waiting for final COMPLETE from client after server push")
		// Check context before blocking receive
		if err := ctx.Err(); err != nil {
			log.Warn().Err(err).Msg("Context cancelled before receiving final COMPLETE")
			return
		}
		finalMsgType, _, err := transport.Receive(ctx)
		if err != nil {
			// Log EOF differently
			if errors.Is(err, io.EOF) {
				log.Warn().Msg("Connection closed by client before sending final COMPLETE")
			} else {
				log.Error().Err(err).Msg("Failed to receive final COMPLETE from client")
			}
			return
		}
		if finalMsgType != miror.MessageTypeComplete {
			log.Error().Uint8("msg_type", finalMsgType).Msg("Expected final COMPLETE from client, got something else")
			// TODO: Send ERROR message?
			return
		}
		log.Info().Msg("Received final COMPLETE from client. Server push successful.")

	case miror.MessageTypeOffer:
		// Client wants to push its objects (handle client push)
		log.Info().Msg("Processing OFFER from client (client push)")
		if err := handleClientPush(ctx, log, transport, objectStore, wal, sessionID, clientRespBody); err != nil {
			log.Error().Err(err).Msg("Failed during client push handling")
			// Error already logged in handleClientPush, just return
			return
		}
		log.Info().Msg("Client push handling successful.")

	case miror.MessageTypeComplete:
		// Client doesn't need anything and isn't pushing anything. Sync is done.
		log.Info().Msg("Received COMPLETE from client immediately, sync finished")

	default:
		log.Error().Uint8("msg_type", msgType).Msg("Received unexpected message type from client after initial server OFFER")
		// TODO: Send ERROR message?
		return
	}

	log.Info().Msg("Synchronization handling complete for this connection")
}

// handleClientPush handles the logic when the client sends an OFFER message.
func handleClientPush(ctx context.Context, log zerolog.Logger, transport miror.Transport, objectStore miror.ObjectStore, wal miror.WAL, sessionID miror.SessionID, offerBody []byte) error {
	log.Debug().Msg("Decoding client OFFER...") // Changed level
	offeredHashes, err := miror.DecodeOfferMessage(offerBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decode client OFFER") // Log error
		return fmt.Errorf("failed to decode client OFFER message: %w", err)
	}
	log.Debug().Int("count", len(offeredHashes)).Msg("Decoded client OFFER")

	// Determine needed hashes
	log.Debug().Msg("Determining needed objects from client OFFER...")
	neededHashes := make([]miror.ObjectHash, 0, len(offeredHashes))
	hashesToReceive := make(map[miror.ObjectHash]struct{})
	for _, hash := range offeredHashes {
		// Check context frequently during potentially long loops
		if err := ctx.Err(); err != nil {
			log.Warn().Err(err).Msg("Context cancelled during needed object check")
			return fmt.Errorf("context cancelled during needed object check: %w", err)
		}
		has, err := objectStore.HasObject(ctx, hash)
		if err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to check object store") // Log error
			return fmt.Errorf("failed to check object store for %s: %w", hash, err)
		}
		if !has {
			log.Debug().Str("hash", hash.String()).Msg("Need object from client")
			neededHashes = append(neededHashes, hash)
			hashesToReceive[hash] = struct{}{}
		} else {
			log.Debug().Str("hash", hash.String()).Msg("Already have object")
		}
	}
	log.Debug().Int("needed", len(neededHashes)).Int("offered", len(offeredHashes)).Msg("Determined needed objects from client OFFER")

	// Send ACCEPT message
	log.Debug().Msg("Encoding ACCEPT message...")
	acceptBody, err := miror.EncodeAcceptMessage(neededHashes)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode ACCEPT message") // Log error
		return fmt.Errorf("failed to encode ACCEPT message: %w", err)
	}
	log.Info().Int("count", len(neededHashes)).Msg("Sending ACCEPT to client")
	if err := ctx.Err(); err != nil { // Check context before send
		log.Warn().Err(err).Msg("Context cancelled before sending ACCEPT")
		return fmt.Errorf("context cancelled before sending ACCEPT: %w", err)
	}
	if err := transport.Send(ctx, miror.MessageTypeAccept, acceptBody); err != nil {
		log.Error().Err(err).Msg("Failed to send ACCEPT message") // Log error
		return fmt.Errorf("failed to send ACCEPT message: %w", err)
	}
	log.Debug().Msg("Sent ACCEPT to client")

	if len(neededHashes) == 0 {
		log.Info().Msg("No objects needed from client, waiting for COMPLETE")
	} else {
		log.Info().Int("count", len(neededHashes)).Msg("Waiting for DATA messages from client")
	}

	// Receive DATA messages until COMPLETE
	receivedCount := 0
	for len(hashesToReceive) > 0 {
		if err := ctx.Err(); err != nil {
			log.Warn().Err(err).Msg("Context cancelled while receiving DATA")
			return fmt.Errorf("context cancelled during data transfer: %w", err)
		}

		log.Debug().Msg("Waiting to receive next message (DATA or COMPLETE)...")
		msgType, dataBody, err := transport.Receive(ctx)
		if err != nil {
			// Log EOF differently
			if errors.Is(err, io.EOF) {
				log.Warn().Msg("Connection closed by client while waiting for DATA/COMPLETE")
			} else {
				log.Error().Err(err).Msg("Failed to receive DATA/COMPLETE message from client") // Log error
			}
			return fmt.Errorf("failed to receive DATA message from client: %w", err)
		}
		log.Debug().Uint8("msg_type", msgType).Int("body_size", len(dataBody)).Msg("Received message from client")

		// Check for COMPLETE
		if msgType == miror.MessageTypeComplete {
			if len(hashesToReceive) == 0 {
				log.Info().Msg("Received final COMPLETE from client as expected (no objects needed or all received)")
				break // Exit loop normally
			} else {
				log.Error().Int("remaining", len(hashesToReceive)).Msg("Received COMPLETE from client unexpectedly before all accepted objects were received") // Log error
				return fmt.Errorf("received COMPLETE from client unexpectedly before all accepted objects were received")
			}
		}

		if msgType != miror.MessageTypeData {
			log.Error().Uint8("msg_type", msgType).Msg("Received unexpected message type from client, expected DATA") // Log error
			return fmt.Errorf("received unexpected message type %d from client, expected DATA", msgType)
		}

		log.Debug().Msg("Decoding DATA message...")
		hash, offset, data, err := miror.DecodeDataMessage(dataBody)
		if err != nil {
			log.Error().Err(err).Msg("Failed to decode DATA message from client") // Log error
			return fmt.Errorf("failed to decode DATA message from client: %w", err)
		}

		if _, ok := hashesToReceive[hash]; !ok {
			log.Error().Str("hash", hash.String()).Msg("Received unexpected object hash in DATA message from client") // Log error
			return fmt.Errorf("received unexpected object hash %s in DATA message from client", hash)
		}

		// TODO: Handle partial transfers using offset (M2)
		if offset != 0 {
			log.Error().Uint64("offset", offset).Str("hash", hash.String()).Msg("Received non-zero offset, partial transfers not supported in M1") // Log error
			return fmt.Errorf("received non-zero offset %d for %s, partial transfers not supported in M1", offset, hash)
		}

		log.Info().Str("hash", hash.String()).Int("size", len(data)).Msg("Received DATA from client")

		// Log receive in WAL
		log.Debug().Str("hash", hash.String()).Msg("Logging receive to WAL...")
		if err := wal.LogReceive(sessionID, hash); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to log receive to WAL") // Log error
			return fmt.Errorf("failed to log receive to WAL for %s: %w", hash, err)
		}

		// Store object
		log.Debug().Str("hash", hash.String()).Msg("Storing object...")
		if err := objectStore.PutObject(ctx, hash, data); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to put object") // Log error
			return fmt.Errorf("failed to put object %s: %w", hash, err)
		}

		// TODO: Send ACK (M2)

		// Complete transfer in WAL
		log.Debug().Str("hash", hash.String()).Msg("Completing transfer in WAL...")
		if err := wal.CompleteTransfer(sessionID, hash); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to complete transfer in WAL") // Log error
			return fmt.Errorf("failed to complete transfer in WAL for %s: %w", hash, err)
		}

		delete(hashesToReceive, hash)
		receivedCount++
		log.Debug().Str("hash", hash.String()).Int("received", receivedCount).Int("remaining", len(hashesToReceive)).Msg("Object processed")
	}

	// If we received objects, we might expect one final COMPLETE message (depends on exact protocol flow if peer sends COMPLETE after last DATA or only if no data was sent)
	// Let's assume the peer sends COMPLETE *after* the last DATA if data was sent.
	// The loop condition `len(hashesToReceive) > 0` will break when the last DATA is processed.
	// If the loop broke because COMPLETE was received, we're good.
	// If the loop broke because len(hashesToReceive) == 0, we now expect a COMPLETE.

	if receivedCount > 0 {
		log.Info().Msg("All expected objects received from client, waiting for final COMPLETE")
		if err := ctx.Err(); err != nil { // Check context before receive
			log.Warn().Err(err).Msg("Context cancelled before receiving final COMPLETE")
			return fmt.Errorf("context cancelled before receiving final COMPLETE: %w", err)
		}
		msgType, _, err := transport.Receive(ctx) // Ignore body for now
		if err != nil {
			// Log EOF differently
			if errors.Is(err, io.EOF) {
				log.Warn().Msg("Connection closed by client before sending final COMPLETE")
			} else {
				log.Error().Err(err).Msg("Failed to receive final COMPLETE message from client") // Log error
			}
			return fmt.Errorf("failed to receive final COMPLETE message from client: %w", err)
		}
		if msgType != miror.MessageTypeComplete {
			log.Error().Uint8("msg_type", msgType).Msg("Received unexpected message type from client, expected final COMPLETE") // Log error
			return fmt.Errorf("received unexpected message type %d from client, expected final COMPLETE", msgType)
		}
		log.Info().Msg("Received final COMPLETE from client")
	}

	log.Info().Int("objects_received", receivedCount).Msg("Client push handling complete")
	return nil
}

// sendObjects sends the specified objects to the peer.
func sendObjects(ctx context.Context, log zerolog.Logger, transport miror.Transport, objectStore miror.ObjectStore, wal miror.WAL, sessionID miror.SessionID, hashesToSend []miror.ObjectHash) error {
	log.Info().Int("count", len(hashesToSend)).Msg("Starting to send objects") // Changed level
	for i, hash := range hashesToSend {
		log.Debug().Str("hash", hash.String()).Int("current", i+1).Int("total", len(hashesToSend)).Msg("Preparing to send object")
		if err := ctx.Err(); err != nil {
			log.Warn().Err(err).Msg("Context cancelled during object send loop")
			return fmt.Errorf("context cancelled during object send: %w", err)
		}

		// Log send
		log.Debug().Str("hash", hash.String()).Msg("Logging send to WAL...")
		if err := wal.LogSend(sessionID, hash); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to log send") // Log error
			return fmt.Errorf("failed to log send for %s: %w", hash, err)
		}

		// Get object data
		log.Debug().Str("hash", hash.String()).Msg("Getting object data...")
		data, err := objectStore.GetObject(ctx, hash)
		if err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to get object") // Log error
			// Special handling for ErrNotFound which might indicate an internal state issue
			if errors.Is(err, dao.ErrNotFound) {
				log.Error().Str("hash", hash.String()).Msg("Object hash found in list but GetObject failed with ErrNotFound!")
			}
			return fmt.Errorf("failed to get object %s: %w", hash, err)
		}

		// Encode DATA message (offset 0 for M1)
		log.Debug().Str("hash", hash.String()).Uint64("offset", 0).Int("size", len(data)).Msg("Encoding DATA message...")
		dataBody, err := miror.EncodeDataMessage(hash, 0, data)
		if err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to encode DATA message") // Log error
			return fmt.Errorf("failed to encode DATA message for %s: %w", hash, err)
		}

		// Send DATA
		log.Info().Str("hash", hash.String()).Int("size", len(data)).Msg("Sending DATA") // Changed level
		if err := ctx.Err(); err != nil {                                                // Check context before send
			log.Warn().Err(err).Msg("Context cancelled before sending DATA")
			return fmt.Errorf("context cancelled before sending DATA: %w", err)
		}
		if err := transport.Send(ctx, miror.MessageTypeData, dataBody); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to send DATA message") // Log error
			return fmt.Errorf("failed to send DATA message for %s: %w", hash, err)
		}

		// TODO: Wait for ACK (M2)

		// Complete transfer in WAL
		log.Debug().Str("hash", hash.String()).Msg("Completing transfer in WAL...")
		if err := wal.CompleteTransfer(sessionID, hash); err != nil {
			log.Error().Err(err).Str("hash", hash.String()).Msg("Failed to complete transfer") // Log error
			return fmt.Errorf("failed to complete transfer for %s: %w", hash, err)
		}
		log.Debug().Str("hash", hash.String()).Msg("Object sent successfully")
	}

	// After sending all data, send COMPLETE
	log.Info().Msg("Finished sending objects, sending COMPLETE") // Changed level
	completeBody, err := miror.EncodeCompleteMessage(sessionID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode COMPLETE message") // Log error
		return fmt.Errorf("failed to encode COMPLETE message: %w", err)
	}
	if err := ctx.Err(); err != nil { // Check context before send
		log.Warn().Err(err).Msg("Context cancelled before sending COMPLETE")
		return fmt.Errorf("context cancelled before sending COMPLETE: %w", err)
	}
	if err := transport.Send(ctx, miror.MessageTypeComplete, completeBody); err != nil {
		log.Error().Err(err).Msg("Failed to send COMPLETE message") // Log error
		return fmt.Errorf("failed to send COMPLETE message: %w", err)
	}
	log.Info().Int("objects_sent", len(hashesToSend)).Msg("Server push complete")
	return nil
}

func main() {
	config := DefaultConfig()

	app := &cli.App{
		Name:  "mirord",
		Usage: "n1 synchronization daemon",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "vault",
				Aliases:     []string{"v"},
				Usage:       "Path to the vault file (required)",
				Destination: &config.VaultPath,
				Required:    true, // Make vault path mandatory
			},
			&cli.StringFlag{
				Name:        "wal-path",
				Aliases:     []string{"w"},
				Usage:       "Path to the WAL directory",
				Value:       DefaultWALPath,
				Destination: &config.WALPath,
			},
			&cli.StringFlag{
				Name:        "pid-file",
				Aliases:     []string{"p"},
				Usage:       "Path to the PID file",
				Value:       DefaultPIDFile,
				Destination: &config.PIDFile,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Aliases:     []string{"l"},
				Usage:       "Logging level (debug, info, warn, error)",
				Value:       "info",
				Destination: &config.LogLevel,
			},
			&cli.StringSliceFlag{
				Name:    "listen",
				Aliases: []string{"L"},
				Usage:   "Addresses to listen on (e.g., :7001)",
				Value:   cli.NewStringSlice(config.ListenAddresses...), // Use default from config
			},
			&cli.StringSliceFlag{
				Name:    "peer",
				Aliases: []string{"P"},
				Usage:   "Known peers to connect to (for client mode, not used by daemon)",
			},
			&cli.BoolFlag{
				Name:        "discovery",
				Aliases:     []string{"d"},
				Usage:       "Enable mDNS discovery (not implemented)",
				Value:       true,
				Destination: &config.DiscoveryEnabled,
			},
			&cli.DurationFlag{
				Name:        "sync-interval",
				Aliases:     []string{"i"},
				Usage:       "Interval for automatic synchronization (not implemented)",
				Value:       5 * time.Minute,
				Destination: &config.SyncInterval,
			},
			&cli.BoolFlag{
				Name:  "verbose", // Add verbose flag for convenience
				Usage: "Enable verbose (debug) logging",
				Value: false,
			},
		},
		Action: func(c *cli.Context) error {
			// Expand paths
			config.WALPath = expandPath(config.WALPath)
			config.PIDFile = expandPath(config.PIDFile)
			config.VaultPath = expandPath(config.VaultPath) // Expand vault path too

			// Get values from string slice flags
			config.ListenAddresses = c.StringSlice("listen")
			config.Peers = c.StringSlice("peer") // Not used by server, but parse anyway

			// Override log level if verbose is set
			if c.Bool("verbose") {
				config.LogLevel = "debug"
			}

			// Run the daemon
			return runDaemon(config)
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Use fmt here as logger might not be initialized or working
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Need to add SetConnection to TCPTransport in internal/miror/transport.go
// func (t *TCPTransport) SetConnection(conn net.Conn) {
// 	t.conn = conn
// }
// Need to add DecodeOfferMessage to internal/miror/miror.go (if not already present)
// func DecodeOfferMessage(data []byte) ([]ObjectHash, error) { ... }
// Need to add EncodeAcceptMessage to internal/miror/miror.go (if not already present)
// func EncodeAcceptMessage(hashes []ObjectHash) ([]byte, error) { ... }
// Need to add DecodeDataMessage to internal/miror/miror.go (if not already present)
// func DecodeDataMessage(data []byte) (ObjectHash, uint64, []byte, error) { ... }
