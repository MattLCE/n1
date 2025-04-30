package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
		// We don't need the key contents for this check, just its presence.
		_, err = secretstore.Default.Get(path)
		if err != nil {
			return fmt.Errorf("failed to get key from secret store (does it exist?): %w", err)
		}
		log.Info().Str("path", path).Msg("Key found in secret store")

		// 2. Try opening the plaintext DB file
		db, err := sqlite.Open(path) // Correct: only path needed
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", path, err)
		}
		// Ping is already done inside Open, but an extra one doesn't hurt
		err = db.Ping()
		if err != nil {
			_ = db.Close()
			return fmt.Errorf("failed to ping database '%s' after opening: %w", path, err)
		}
		defer db.Close() // Ensure DB is closed

		// TODO: Later, add logic here to fetch key and attempt to decrypt a sample piece of data.
		log.Info().Str("path", path).Msg("Vault check complete: Key exists and database file is accessible")
		return nil
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

		// 1. Get old key from store
		oldMK, err := secretstore.Default.Get(path)
		if err != nil {
			return fmt.Errorf("failed to get current key from secret store: %w", err)
		}
		log.Info().Msg("Retrieved current master key")

		// 2. Open plaintext DB
		db, err := sqlite.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open database file '%s': %w", path, err)
		}
		defer db.Close()
		log.Info().Str("path", path).Msg("Opened database file")

		// 3. Create a secure vault DAO with the old key
		vault := dao.NewSecureVaultDAO(db, oldMK)

		// 4. List all keys in the vault
		keys, err := vault.List()
		if err != nil {
			return fmt.Errorf("failed to list vault keys: %w", err)
		}
		log.Info().Int("count", len(keys)).Msg("Found keys in vault")

		if dryRun {
			// In dry-run mode, just list the keys that would be re-encrypted
			log.Info().Msg("The following keys would be re-encrypted:")
			for _, k := range keys {
				log.Info().Str("key", k).Msg("Would re-encrypt")
			}
			log.Info().Msg("Dry run completed successfully. No changes were made.")
			return nil
		}

		// 5. Generate new key
		newMK, err := crypto.Generate(32)
		if err != nil {
			return fmt.Errorf("failed to generate new master key: %w", err)
		}
		log.Info().Msg("Generated new master key")

		// 6. Re-encrypt all values with the new key
		log.Info().Msg("Re-encrypting vault data...")
		for i, k := range keys {
			if i%10 == 0 || i == len(keys)-1 {
				log.Debug().Int("progress", i+1).Int("total", len(keys)).Msg("Re-encryption progress")
			}

			// Get and decrypt with old key
			plaintext, err := vault.Get(k)
			if err != nil {
				return fmt.Errorf("failed to get value for key %s during rotation: %w", k, err)
			}

			// Encrypt with new key
			ciphertext, err := crypto.EncryptBlob(newMK, plaintext)
			if err != nil {
				return fmt.Errorf("failed to encrypt value for key %s during rotation: %w", k, err)
			}

			// Update the record directly
			_, err = db.Exec(
				"UPDATE vault SET value = ? WHERE key = ?",
				ciphertext, k,
			)
			if err != nil {
				return fmt.Errorf("failed to update value for key %s during rotation: %w", k, err)
			}
		}
		log.Info().Int("count", len(keys)).Msg("Re-encrypted keys")

		// 7. Update key in secret store
		if err := secretstore.Default.Put(path, newMK); err != nil {
			return fmt.Errorf("failed to store new master key: %w", err)
		}
		log.Info().Msg("Updated master key in secret store")

		log.Info().Msg("Key rotation completed successfully")
		return nil
	},
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
