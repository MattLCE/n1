package main

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	// Internal packages
	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/migrations"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const version = "0.0.1-dev"

func main() {
	app := &cli.App{
		Name:    "bosr",
		Version: version,
		Usage:   "bosr – the n1 lock-box CLI",
		Commands: []*cli.Command{
			initCmd,
			openCmd,
			keyCmd, // Keep the top-level key command structure
			putCmd,
			getCmd,
			syncCmd, // Add the sync command
		},
	}

	// Configure logging
	if os.Getenv("DEBUG") == "1" {
		log.SetLevel(zerolog.DebugLevel)
		log.EnableConsoleOutput()
		log.Debug().Msg("Debug logging enabled")
	} else {
		log.SetLevel(zerolog.InfoLevel)
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("Application error")
	}
}

/* ----------------- commands ----------------- */

var initCmd = &cli.Command{
	Name:      "init",
	Usage:     "init <vault.db>   – create plaintext vault file and store its key",
	ArgsUsage: "<path>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 1 {
			return cli.Exit("Usage: init <vault.db>", 1)
		}
		path, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		// Check if DB or key already exists to prevent overwriting? (Optional)
		// if _, err := os.Stat(path); err == nil {
		//     return fmt.Errorf("database file already exists: %s", path)
		// }
		// if _, err := secretstore.Default.Get(path); err == nil {
		//     return fmt.Errorf("key already exists for path: %s", path)
		// }

		// 1· generate master-key (for application-level encryption)
		mk, err := crypto.Generate(32)
		if err != nil {
			return fmt.Errorf("failed to generate master key: %w", err)
		}

		// 2· persist in secret store
		if err = secretstore.Default.Put(path, mk); err != nil {
			// Consider if we should attempt cleanup if this fails
			return fmt.Errorf("failed to store master key: %w", err)
		}
		log.Info().Str("path", path).Msg("Master key generated and stored")

		// 3· create *plaintext* DB file by opening it
		// The Open function now only takes the path.
		db, err := sqlite.Open(path)
		if err != nil {
			// If DB creation fails, should we remove the key we just stored?
			_ = secretstore.Default.Delete(path) // Cleanup key if DB creation fails
			return fmt.Errorf("failed to create database file '%s': %w", path, err)
		}
		defer db.Close() // Ensure DB is closed

		// 4· Run migrations to bootstrap the vault table
		log.Info().Msg("Running migrations to initialize vault schema...")
		if err := migrations.BootstrapVault(db); err != nil {
			// If migrations fail, clean up
			_ = secretstore.Default.Delete(path)
			return fmt.Errorf("failed to initialize vault schema: %w", err)
		}

		// Add a canary record for key verification
		secureDAO := dao.NewSecureVaultDAO(db, mk)
		canaryKey := "__n1_canary__"
		canaryPlaintext := []byte("ok")
		if err := secureDAO.Put(canaryKey, canaryPlaintext); err != nil {
			// If canary creation fails, clean up
			_ = secretstore.Default.Delete(path)
			return fmt.Errorf("failed to create canary record: %w", err)
		}
		log.Debug().Msg("Added canary record for key verification")

		log.Info().Str("path", path).Msg("Plaintext vault file created and initialized")
		return nil
	},
}

var openCmd = &cli.Command{
	Name:      "open",
	Usage:     "open <vault.db>     – check key exists and DB file is accessible",
	ArgsUsage: "<path>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 1 {
			return cli.Exit("Usage: open <vault.db>", 1)
		}
		path, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		// 1. Check if the key exists in the secret store
		mk, err := secretstore.Default.Get(path)
		if err != nil {
			return fmt.Errorf("failed to get key from secret store (does it exist?): %w", err)
		}
		log.Info().Str("path", path).Msg("Key found in secret store")

		// 2. Try opening the plaintext DB file
		db, err := sqlite.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", path, err)
		}
		defer db.Close() // Ensure DB is closed

		// 3. Verify the key can decrypt data in the vault
		secureDAO := dao.NewSecureVaultDAO(db, mk)
		canaryKey := "__n1_canary__"
		plaintext, err := secureDAO.Get(canaryKey)

		if err == nil && string(plaintext) == "ok" {
			log.Info().Str("path", path).Msg("✓ Vault check complete: Key verified and database accessible.")
			return nil
		} else if errors.Is(err, dao.ErrNotFound) {
			return fmt.Errorf("vault key found, but integrity check failed (canary missing). Vault may be incomplete or corrupt")
		} else if err != nil {
			// Check if it's a crypto error (decryption failure)
			if strings.Contains(err.Error(), "failed to decrypt") {
				return fmt.Errorf("vault key found, but decryption failed. Key may be incorrect or data corrupted")
			}
			return fmt.Errorf("vault check failed: %w", err)
		}

		return fmt.Errorf("vault check failed: unexpected canary value")
	},
}

// Keep the top-level 'key' command structure
var keyCmd = &cli.Command{
	Name:  "key",
	Usage: "key <subcommand> <vault.db> – manage vault key",
	Subcommands: []*cli.Command{
		keyRotateCmd,
		// Add other key management subcommands here later (e.g., key show, key export)
	},
}

// Key rotation implementation
var keyRotateCmd = &cli.Command{
	Name:      "rotate",
	Usage:     "rotate <vault.db>  – create new key & re-encrypt data",
	ArgsUsage: "<path>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Simulate key rotation without making changes",
			Value: false,
		},
	},
	Action: func(c *cli.Context) error {
		if c.NArg() != 1 {
			return cli.Exit("Usage: key rotate [--dry-run] <vault.db>", 1)
		}
		path, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		dryRun := c.Bool("dry-run")
		if dryRun {
			fmt.Println("Running in dry-run mode - no changes will be made")
		}

		// 1. Pre-flight checks
		originalPath := path
		backupPath := originalPath + ".bak"
		tempPath := originalPath + ".tmp"

		// Check if backup or temp files already exist
		if _, err := os.Stat(backupPath); err == nil {
			return fmt.Errorf("backup file %s already exists; please remove it before proceeding", backupPath)
		}
		if _, err := os.Stat(tempPath); err == nil {
			return fmt.Errorf("temporary file %s already exists; please remove it before proceeding", tempPath)
		}

		// Check original file exists
		originalInfo, err := os.Stat(originalPath)
		if err != nil {
			return fmt.Errorf("cannot access original vault at %s: %w", originalPath, err)
		}

		// Check available disk space
		originalSize := originalInfo.Size()
		requiredSpace := originalSize * 3 // Original + backup + temp

		// Get available disk space (platform-specific)
		var stat syscall.Statfs_t
		if err := syscall.Statfs(filepath.Dir(originalPath), &stat); err != nil {
			log.Warn().Err(err).Msg("Could not check available disk space")
		} else {
			availableSpace := stat.Bavail * uint64(stat.Bsize)
			if uint64(requiredSpace) > availableSpace {
				return fmt.Errorf("insufficient disk space: need approximately %d bytes, have %d bytes available",
					requiredSpace, availableSpace)
			}
		}

		// Warn if file is large
		if originalSize > 1024*1024*1024 { // 1GB
			log.Warn().Int64("size_bytes", originalSize).Msg("Vault file is very large, rotation may take significant time and disk space")
			fmt.Print("Vault file is large (>1GB). Continue with rotation? (y/N): ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read user input: %w", err)
			}
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				return fmt.Errorf("key rotation cancelled by user")
			}
		}

		// 2. Get old key from store
		oldMK, err := secretstore.Default.Get(originalPath)
		if err != nil {
			return fmt.Errorf("failed to get current key from secret store: %w", err)
		}
		log.Info().Msg("Retrieved current master key")

		// 3. Generate new key
		newMK, err := crypto.Generate(32)
		if err != nil {
			return fmt.Errorf("failed to generate new master key: %w", err)
		}
		log.Info().Msg("Generated new master key")

		// Open original DB to list keys
		originalDB, err := sqlite.Open(originalPath)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", originalPath, err)
		}

		// Create a secure vault DAO with the old key
		oldSecureDAO := dao.NewSecureVaultDAO(originalDB, oldMK)

		// List all keys in the vault
		keys, err := oldSecureDAO.List()
		if err != nil {
			originalDB.Close()
			return fmt.Errorf("failed to list vault keys: %w", err)
		}
		log.Info().Int("count", len(keys)).Msg("Found keys in vault")

		if dryRun {
			// In dry-run mode, just list the keys that would be re-encrypted
			log.Info().Msg("The following keys would be re-encrypted:")
			for _, k := range keys {
				log.Info().Str("key", k).Msg("Would re-encrypt")
			}
			originalDB.Close()
			log.Info().Msg("Dry run completed successfully. No changes were made.")
			return nil
		}

		// Close the original DB before copying
		originalDB.Close()

		// 4. Create backup
		log.Info().Str("backup_path", backupPath).Msg("Creating backup of original vault...")
		if err := copyFile(originalPath, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		log.Info().Msg("Backup created successfully")

		// Function to clean up on failure
		cleanup := func(keepBackup bool) {
			log.Debug().Msg("Running cleanup...")
			if _, err := os.Stat(tempPath); err == nil {
				if err := os.Remove(tempPath); err != nil {
					log.Warn().Err(err).Str("path", tempPath).Msg("Failed to remove temporary file during cleanup")
				} else {
					log.Debug().Str("path", tempPath).Msg("Removed temporary file")
				}
			}

			if !keepBackup {
				if _, err := os.Stat(backupPath); err == nil {
					if err := os.Remove(backupPath); err != nil {
						log.Warn().Err(err).Str("path", backupPath).Msg("Failed to remove backup file during cleanup")
					} else {
						log.Debug().Str("path", backupPath).Msg("Removed backup file")
					}
				}
			}
		}

		// 5. Setup temp DB
		log.Info().Str("temp_path", tempPath).Msg("Creating temporary database...")
		tempDB, err := sqlite.Open(tempPath)
		if err != nil {
			cleanup(true) // Keep backup on failure
			return fmt.Errorf("failed to create temporary database: %w", err)
		}

		// Initialize schema in temp DB
		if err := migrations.BootstrapVault(tempDB); err != nil {
			tempDB.Close()
			cleanup(true) // Keep backup on failure
			return fmt.Errorf("failed to initialize schema in temporary database: %w", err)
		}

		// 6. Open original DB again
		originalDB, err = sqlite.Open(originalPath)
		if err != nil {
			tempDB.Close()
			cleanup(true) // Keep backup on failure
			return fmt.Errorf("failed to reopen original database: %w", err)
		}

		// 7. Migrate data with progress
		log.Info().Msg("Migrating data to temporary database with new key...")
		oldSecureDAO = dao.NewSecureVaultDAO(originalDB, oldMK)
		tempRawDAO := dao.NewVaultDAO(tempDB)

		for i, k := range keys {
			// Show progress
			log.Info().Msgf("Migrating data... %d / %d", i+1, len(keys))

			// Get and decrypt with old key
			plaintext, err := oldSecureDAO.Get(k)
			if err != nil {
				originalDB.Close()
				tempDB.Close()
				cleanup(true) // Keep backup on failure
				return fmt.Errorf("failed to get value for key %s during rotation: %w", k, err)
			}

			// Encrypt with new key
			newCiphertext, err := crypto.EncryptBlob(newMK, plaintext)
			if err != nil {
				originalDB.Close()
				tempDB.Close()
				cleanup(true) // Keep backup on failure
				return fmt.Errorf("failed to encrypt value for key %s during rotation: %w", k, err)
			}

			// Store in temp DB
			err = tempRawDAO.Put(k, newCiphertext)
			if err != nil {
				originalDB.Close()
				tempDB.Close()
				cleanup(true) // Keep backup on failure
				return fmt.Errorf("failed to store value for key %s in temporary database: %w", k, err)
			}
		}

		// 8. Close DBs
		originalDB.Close()
		tempDB.Close()
		log.Info().Msg("Data migration completed successfully")

		// 9. Update key store
		log.Info().Msg("Updating key store with new master key...")
		if err := secretstore.Default.Put(originalPath, newMK); err != nil {
			cleanup(true) // Keep backup on failure
			return fmt.Errorf("failed to update master key in secret store: %w", err)
		}
		log.Info().Msg("Key store updated successfully")

		// 10. Atomic replace
		log.Info().Msg("Replacing original vault with new vault...")
		if err := os.Rename(tempPath, originalPath); err != nil {
			// Critical failure: key store has new key but original DB is still old
			log.Error().Err(err).Msg("CRITICAL: Failed to replace original vault with new vault")
			log.Error().Msg("The key store has been updated with the new key, but the rename operation failed.")
			log.Error().Msgf("You need to manually rename %s to %s", tempPath, originalPath)
			return fmt.Errorf("failed to replace original vault with new vault: %w", err)
		}

		// 11. Delete backup
		log.Info().Msg("Removing backup file...")
		if err := os.Remove(backupPath); err != nil {
			log.Warn().Err(err).Msg("Failed to remove backup file, but key rotation was successful")
			log.Warn().Msgf("You may want to manually remove the backup file: %s", backupPath)
		}

		// 12. Report success
		log.Info().Msg("Key rotation completed successfully")
		return nil
	},
}

// Helper function to copy a file
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	buf := make([]byte, 1024*1024) // 1MB buffer
	for {
		n, err := sourceFile.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("error reading from source file: %w", err)
		}
		if n == 0 {
			break
		}

		if _, err := destFile.Write(buf[:n]); err != nil {
			return fmt.Errorf("error writing to destination file: %w", err)
		}
	}

	return nil
}

// Helper function to create a SecureVaultDAO
func NewSecureVaultDAO(db *sql.DB, key []byte) *dao.SecureVaultDAO {
	return dao.NewSecureVaultDAO(db, key)
}

var putCmd = &cli.Command{
	Name:      "put",
	Usage:     "put <vault.db> <key> <value>  – store an encrypted value",
	ArgsUsage: "<path> <key> <value>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 3 {
			return cli.Exit("Usage: put <vault.db> <key> <value>", 1)
		}
		path, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		key := c.Args().Get(1)
		value := c.Args().Get(2)

		// 1. Get the master key from the secret store
		mk, err := secretstore.Default.Get(path)
		if err != nil {
			return fmt.Errorf("failed to get key from secret store: %w", err)
		}

		// 2. Open the database
		db, err := sqlite.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", path, err)
		}
		defer db.Close()

		// 3. Create a secure vault DAO
		vault := dao.NewSecureVaultDAO(db, mk)

		// 4. Store the value
		if err := vault.Put(key, []byte(value)); err != nil {
			return fmt.Errorf("failed to store value: %w", err)
		}

		log.Info().Str("key", key).Msg("Value stored successfully")
		return nil
	},
}

var getCmd = &cli.Command{
	Name:      "get",
	Usage:     "get <vault.db> <key>  – retrieve an encrypted value",
	ArgsUsage: "<path> <key>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 2 {
			return cli.Exit("Usage: get <vault.db> <key>", 1)
		}
		path, err := filepath.Abs(c.Args().First())
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		key := c.Args().Get(1)

		// 1. Get the master key from the secret store
		mk, err := secretstore.Default.Get(path)
		if err != nil {
			return fmt.Errorf("failed to get key from secret store: %w", err)
		}

		// 2. Open the database
		db, err := sqlite.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", path, err)
		}
		defer db.Close()

		// 3. Create a secure vault DAO
		vault := dao.NewSecureVaultDAO(db, mk)

		// 4. Retrieve the value
		value, err := vault.Get(key)
		if err != nil {
			if errors.Is(err, dao.ErrNotFound) {
				return fmt.Errorf("key '%s' not found", key)
			}
			return fmt.Errorf("failed to retrieve value: %w", err)
		}

		// Still print the value to stdout for CLI usage
		fmt.Printf("%s\n", string(value))
		log.Debug().Str("key", key).Int("value_size", len(value)).Msg("Value retrieved successfully")
		return nil
	},
}
