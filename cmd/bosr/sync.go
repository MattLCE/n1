package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/miror"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
	"github.com/n1/n1/internal/vaultid"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

// ObjectStoreAdapter adapts the vault DAO to the miror.ObjectStore interface
type ObjectStoreAdapter struct {
	db        *sql.DB
	vaultPath string
	secureDAO *dao.SecureVaultDAO
	// hashToKey maps object hashes to their keys in the vault
	hashToKey map[string]string
	// keyToHash maps keys to their content hashes
	keyToHash map[string]miror.ObjectHash
}

// NewObjectStoreAdapter creates a new adapter for the vault
func NewObjectStoreAdapter(db *sql.DB, vaultPath string, masterKey []byte) *ObjectStoreAdapter {
	adapter := &ObjectStoreAdapter{
		db:        db,
		vaultPath: vaultPath,
		secureDAO: dao.NewSecureVaultDAO(db, masterKey),
		hashToKey: make(map[string]string),
		keyToHash: make(map[string]miror.ObjectHash),
	}

	// Initialize the hash mappings
	adapter.initHashMappings()

	return adapter
}

// initHashMappings initializes the hash-to-key and key-to-hash mappings
func (a *ObjectStoreAdapter) initHashMappings() {
	// List all keys in the vault
	keys, err := a.secureDAO.List()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list keys during initialization")
		return
	}

	// Build the mappings
	for _, key := range keys {
		// Skip the canary record
		if key == "__n1_canary__" {
			continue
		}

		// Get the encrypted value
		encryptedValue, err := a.secureDAO.Get(key)
		if err != nil {
			log.Error().Err(err).Str("key", key).Msg("Failed to get value during initialization")
			continue
		}

		// Compute the hash of the encrypted value
		hash := a.computeObjectHash(encryptedValue)
		hashStr := hash.String()

		// Store the mappings
		a.hashToKey[hashStr] = key
		a.keyToHash[key] = hash
	}
}

// computeObjectHash computes the SHA-256 hash of the encrypted value
func (a *ObjectStoreAdapter) computeObjectHash(encryptedValue []byte) miror.ObjectHash {
	var hash miror.ObjectHash
	h := sha256.Sum256(encryptedValue)
	copy(hash[:], h[:])
	return hash
}

// GetObject gets an object by its hash
func (a *ObjectStoreAdapter) GetObject(ctx context.Context, hash miror.ObjectHash) ([]byte, error) {
	hashStr := hash.String()

	// Look up the key for this hash
	key, exists := a.hashToKey[hashStr]
	if !exists {
		return nil, dao.ErrNotFound
	}

	// Get the encrypted value
	encryptedValue, err := a.secureDAO.Get(key)
	if err != nil {
		return nil, err
	}

	// Verify the hash matches
	computedHash := a.computeObjectHash(encryptedValue)
	if computedHash.String() != hashStr {
		return nil, fmt.Errorf("hash mismatch for key %s", key)
	}

	// Get and decrypt the value
	return a.secureDAO.Get(key)
}

// PutObject puts an object with the given hash and data
func (a *ObjectStoreAdapter) PutObject(ctx context.Context, hash miror.ObjectHash, data []byte) error {
	// First, try to get the vault ID
	vaultID, err := vaultid.GetVaultIDFromPath(a.vaultPath)
	var masterKey []byte

	if err == nil {
		// If vault ID exists, try to get the key using the vault ID
		secretName := vaultid.FormatSecretName(vaultID)
		masterKey, err = secretstore.Default.Get(secretName)
		if err != nil {
			// Fall back to path-based method
			log.Debug().Err(err).Str("vault_id", vaultID).Msg("Failed to get key using vault ID, falling back to path-based method")
			masterKey, err = secretstore.Default.Get(a.vaultPath)
			if err != nil {
				return fmt.Errorf("failed to get master key: %w", err)
			}
		} else {
			log.Debug().Str("vault_id", vaultID).Msg("Retrieved master key using vault ID")
		}
	} else {
		// If vault ID doesn't exist, try to get the key using the path
		log.Debug().Err(err).Msg("Failed to get vault ID, falling back to path-based method")
		masterKey, err = secretstore.Default.Get(a.vaultPath)
		if err != nil {
			return fmt.Errorf("failed to get master key: %w", err)
		}
	}

	encryptedData, err := crypto.EncryptBlob(masterKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Compute the hash of the encrypted data
	computedHash := a.computeObjectHash(encryptedData)

	// Verify the hash matches what was provided
	if !bytes.Equal(computedHash[:], hash[:]) {
		return fmt.Errorf("hash mismatch: expected %s, got %s", hash.String(), computedHash.String())
	}

	// Use the hash as the key
	key := hash.String()

	// Store the mappings
	a.hashToKey[key] = key
	a.keyToHash[key] = hash

	// Store the data
	return a.secureDAO.Put(key, data)
}

// HasObject checks if an object exists
func (a *ObjectStoreAdapter) HasObject(ctx context.Context, hash miror.ObjectHash) (bool, error) {
	hashStr := hash.String()
	_, exists := a.hashToKey[hashStr]
	return exists, nil
}

// ListObjects lists all object hashes
func (a *ObjectStoreAdapter) ListObjects(ctx context.Context) ([]miror.ObjectHash, error) {
	var hashes []miror.ObjectHash

	// Use the precomputed hashes from our mapping
	for _, hash := range a.keyToHash {
		hashes = append(hashes, hash)
	}

	return hashes, nil
}

// GetObjectReader gets a reader for an object
func (a *ObjectStoreAdapter) GetObjectReader(ctx context.Context, hash miror.ObjectHash) (io.ReadCloser, error) {
	data, err := a.GetObject(ctx, hash)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetObjectWriter gets a writer for an object
func (a *ObjectStoreAdapter) GetObjectWriter(ctx context.Context, hash miror.ObjectHash) (io.WriteCloser, error) {
	// Create a buffer to collect the data
	buf := &bytes.Buffer{}

	// Return a writer that writes to the buffer and then to the object store when closed
	return &objectWriter{
		buffer:      buf,
		hash:        hash,
		objectStore: a,
		ctx:         ctx,
	}, nil
}

// objectWriter is a WriteCloser that writes to a buffer and then to the object store when closed
type objectWriter struct {
	buffer      *bytes.Buffer
	hash        miror.ObjectHash
	objectStore *ObjectStoreAdapter
	ctx         context.Context
}

func (w *objectWriter) Write(p []byte) (n int, err error) {
	return w.buffer.Write(p)
}

func (w *objectWriter) Close() error {
	// When closing the writer, we compute the actual hash of the encrypted data
	// and verify it matches the expected hash
	data := w.buffer.Bytes()

	// Try to get the vault ID
	vaultID, err := vaultid.GetVaultIDFromPath(w.objectStore.vaultPath)
	var masterKey []byte

	if err == nil {
		// If vault ID exists, try to get the key using the vault ID
		secretName := vaultid.FormatSecretName(vaultID)
		masterKey, err = secretstore.Default.Get(secretName)
		if err != nil {
			// Fall back to path-based method
			log.Debug().Err(err).Str("vault_id", vaultID).Msg("Failed to get key using vault ID, falling back to path-based method")
			masterKey, err = secretstore.Default.Get(w.objectStore.vaultPath)
			if err != nil {
				return fmt.Errorf("failed to get master key: %w", err)
			}
		} else {
			log.Debug().Str("vault_id", vaultID).Msg("Retrieved master key using vault ID")
		}
	} else {
		// If vault ID doesn't exist, try to get the key using the path
		log.Debug().Err(err).Msg("Failed to get vault ID, falling back to path-based method")
		masterKey, err = secretstore.Default.Get(w.objectStore.vaultPath)
		if err != nil {
			return fmt.Errorf("failed to get master key: %w", err)
		}
	}

	// Encrypt the data
	encryptedData, err := crypto.EncryptBlob(masterKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Compute the hash of the encrypted data
	computedHash := w.objectStore.computeObjectHash(encryptedData)

	// If the hash doesn't match, we need to update it
	if !bytes.Equal(computedHash[:], w.hash[:]) {
		log.Warn().
			Str("expected", w.hash.String()).
			Str("computed", computedHash.String()).
			Msg("Hash mismatch in objectWriter.Close(), using computed hash")

		w.hash = computedHash
	}

	// Store the object with the correct hash
	return w.objectStore.PutObject(w.ctx, w.hash, data)
}

// syncCmd is the command for synchronizing vaults
var syncCmd = &cli.Command{
	Name:  "sync",
	Usage: "sync <vault.db> <peer> [options] – synchronize with another vault",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "follow",
			Aliases: []string{"f"},
			Usage:   "Continuously synchronize with the peer",
			Value:   false,
		},
		&cli.BoolFlag{
			Name:    "push",
			Aliases: []string{"p"},
			Usage:   "Push changes to the peer (default is pull)",
			Value:   false,
		},
		&cli.StringFlag{
			Name:    "wal-path",
			Aliases: []string{"w"},
			Usage:   "Path to the WAL directory",
			Value:   "~/.local/share/n1/sync/wal",
		},
		&cli.IntFlag{
			Name:    "timeout",
			Aliases: []string{"t"},
			Usage:   "Timeout in seconds for the operation",
			Value:   60,
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Enable verbose output",
			Value:   false,
		},
	},
	Action: func(c *cli.Context) error {
		if c.NArg() != 2 {
			return cli.Exit("Usage: sync <vault.db> <peer> [options]", 1)
		}

		// Parse arguments
		vaultPath, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		peer := c.Args().Get(1)

		// Parse flags
		follow := c.Bool("follow")
		push := c.Bool("push")
		walPath := c.String("wal-path")
		timeout := c.Int("timeout")
		verbose := c.Bool("verbose")

		// Expand paths
		if walPath[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			walPath = filepath.Join(home, walPath[1:])
		}

		// Set log level
		if verbose {
			log.SetLevel(zerolog.DebugLevel)
		}

		// Get the vault ID and use it to get the master key from the secret store
		db, err := sqlite.Open(vaultPath)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", vaultPath, err)
		}
		defer db.Close()

		// Get the vault ID
		vaultID, err := vaultid.GetVaultID(db)
		if err != nil {
			// Fall back to path-based method if vault ID is not available
			log.Warn().Err(err).Msg("Failed to get vault ID, falling back to path-based method")
			mk, err := secretstore.Default.Get(vaultPath)
			if err != nil {
				return fmt.Errorf("failed to get key from secret store: %w", err)
			}
			return runSync(c, vaultPath, peer, follow, push, walPath, timeout, verbose, mk)
		}

		// Format the secret name using the vault ID
		secretName := vaultid.FormatSecretName(vaultID)
		log.Info().Str("vault_id", vaultID).Msg("Using vault ID for key retrieval")

		// Get the master key using the vault ID-based secret name
		mk, err := secretstore.Default.Get(secretName)
		if err != nil {
			// Fall back to path-based method if vault ID-based retrieval fails
			log.Warn().Err(err).Str("vault_id", vaultID).Msg("Failed to get key using vault ID, falling back to path-based method")
			mk, err := secretstore.Default.Get(vaultPath)
			if err != nil {
				return fmt.Errorf("failed to get key from secret store: %w", err)
			}
			return runSync(c, vaultPath, peer, follow, push, walPath, timeout, verbose, mk)
		}

		return runSync(c, vaultPath, peer, follow, push, walPath, timeout, verbose, mk)
	},
}

// runSync runs the sync operation with the given parameters
func runSync(c *cli.Context, vaultPath, peer string, follow, push bool, walPath string, timeout int, verbose bool, mk []byte) error {
	// Open the database
	db, err := sqlite.Open(vaultPath)
	if err != nil {
		return fmt.Errorf("failed to open database file '%s': %w", vaultPath, err)
	}
	defer db.Close()

	// Create the object store adapter
	objectStore := NewObjectStoreAdapter(db, vaultPath, mk)

	// Create the WAL
	wal, err := miror.NewWAL(walPath, 1024*1024) // 1MB sync interval
	if err != nil {
		return fmt.Errorf("failed to create WAL: %w", err)
	}
	defer wal.Close()

	// Create the sync configuration
	syncConfig := miror.DefaultSyncConfig()
	if push {
		syncConfig.Mode = miror.SyncModePush
	} else {
		syncConfig.Mode = miror.SyncModePull
	}
	if follow {
		syncConfig.Mode = miror.SyncModeFollow
	}

	// Create the replicator
	replicator := miror.NewReplicator(syncConfig, objectStore, wal)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Handle signals for graceful shutdown
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalCh
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// Progress callback
	progress := func(current, total int64, objectHash miror.ObjectHash) {
		if verbose || total > 1024*1024 { // Always show progress for transfers > 1MB
			percent := float64(current) / float64(total) * 100
			log.Info().
				Int64("current", current).
				Int64("total", total).
				Float64("percent", percent).
				Str("object", objectHash.String()).
				Msg("Sync progress")
		}
	}

	// Perform the sync operation
	log.Info().
		Str("vault", vaultPath).
		Str("peer", peer).
		Str("mode", syncConfig.Mode.String()).
		Msg("Starting synchronization")

	err = replicator.SyncWithProgress(ctx, peer, syncConfig, progress)
	if err != nil {
		return fmt.Errorf("synchronization failed: %w", err)
	}

	log.Info().Msg("Synchronization completed successfully")
	return nil
}
