package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/miror"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

// ObjectStoreAdapter adapts the vault DAO to the miror.ObjectStore interface
type ObjectStoreAdapter struct {
	db        *sql.DB
	vaultPath string
	secureDAO *dao.SecureVaultDAO
}

// NewObjectStoreAdapter creates a new adapter for the vault
func NewObjectStoreAdapter(db *sql.DB, vaultPath string, masterKey []byte) *ObjectStoreAdapter {
	return &ObjectStoreAdapter{
		db:        db,
		vaultPath: vaultPath,
		secureDAO: dao.NewSecureVaultDAO(db, masterKey),
	}
}

// GetObject gets an object by its hash
func (a *ObjectStoreAdapter) GetObject(ctx context.Context, hash miror.ObjectHash) ([]byte, error) {
	// Implementation would convert the hash to a key and retrieve from the vault
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
}

// PutObject puts an object with the given hash and data
func (a *ObjectStoreAdapter) PutObject(ctx context.Context, hash miror.ObjectHash, data []byte) error {
	// Implementation would convert the hash to a key and store in the vault
	// This is a placeholder implementation
	return fmt.Errorf("not implemented")
}

// HasObject checks if an object exists
func (a *ObjectStoreAdapter) HasObject(ctx context.Context, hash miror.ObjectHash) (bool, error) {
	// Implementation would convert the hash to a key and check if it exists in the vault
	// This is a placeholder implementation
	return false, fmt.Errorf("not implemented")
}

// ListObjects lists all object hashes
func (a *ObjectStoreAdapter) ListObjects(ctx context.Context) ([]miror.ObjectHash, error) {
	// Implementation would list all keys in the vault and convert them to hashes
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
}

// GetObjectReader gets a reader for an object
func (a *ObjectStoreAdapter) GetObjectReader(ctx context.Context, hash miror.ObjectHash) (io.ReadCloser, error) {
	// Implementation would get a reader for the object
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
}

// GetObjectWriter gets a writer for an object
func (a *ObjectStoreAdapter) GetObjectWriter(ctx context.Context, hash miror.ObjectHash) (io.WriteCloser, error) {
	// Implementation would get a writer for the object
	// This is a placeholder implementation
	return nil, fmt.Errorf("not implemented")
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

		// Get the master key from the secret store
		mk, err := secretstore.Default.Get(vaultPath)
		if err != nil {
			return fmt.Errorf("failed to get key from secret store: %w", err)
		}

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
	},
}
