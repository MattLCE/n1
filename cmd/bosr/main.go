package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	// Assuming these are still needed for key generation and storage
	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/secretstore"

	// Uses the updated sqlite package
	"github.com/n1/n1/internal/sqlite"

	"github.com/urfave/cli/v2" // Assuming CLI library is still used
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
		},
	}

	// Consider using a structured logger later, e.g., from internal/log
	log.SetFlags(0) // Remove timestamp prefix from standard logger

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("Error: %v", err) // Print error more clearly
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
		fmt.Printf("✓ Master key generated and stored for %s\n", path)

		// 3· create *plaintext* DB file by opening it
		// The Open function now only takes the path.
		db, err := sqlite.Open(path)
		if err != nil {
			// If DB creation fails, should we remove the key we just stored?
			// _ = secretstore.Default.Delete(path) // Optional cleanup
			return fmt.Errorf("failed to create database file '%s': %w", path, err)
		}
		defer db.Close() // Ensure DB is closed

		// Optional: Initialize minimal schema if needed?
		// _, err = db.Exec(`CREATE TABLE IF NOT EXISTS some_initial_table (...)`)
		// if err != nil {
		// 	return fmt.Errorf("failed to initialize schema: %w", err)
		// }

		fmt.Printf("✓ Plaintext vault file created: %s\n", path)
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
		fmt.Printf("✓ Key found in secret store for %s\n", path)

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
		fmt.Printf("✓ Vault check complete: Key exists and database file '%s' is accessible.\n", path)
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

// Stub out key rotation - Requires complete redesign
var keyRotateCmd = &cli.Command{
	Name:      "rotate",
	Usage:     "rotate <vault.db>  – [NOT IMPLEMENTED] create new key & re-encrypt data",
	ArgsUsage: "<path>",
	Action: func(c *cli.Context) error {
		if c.NArg() != 1 {
			return cli.Exit("Usage: key rotate <vault.db>", 1)
		}
		// path, err := filepath.Abs(c.Args().First())
		// if err != nil {
		//    return fmt.Errorf("failed to get absolute path: %w", err)
		// }

		// --- The old logic below is completely wrong for application-level encryption ---
		// oldMK, err := secretstore.Default.Get(path)
		// if err != nil { return err }
		// db, err := sqlite.Open(path) // Open plaintext
		// if err != nil { return err }
		// defer db.Close()
		// newMK, _ := crypto.Generate(32)
		// // PRAGMA rekey DOES NOT WORK on plaintext DBs or with application encryption
		// // if _, err := db.Exec(fmt.Sprintf("PRAGMA rekey = \"x'%x'\";", newMK)); err != nil { return err }
		// if err := secretstore.Default.Put(path, newMK); err != nil { return err }
		// fmt.Println("✓ key rotated")

		// TODO: Implement application-level rotation:
		// 1. Get old key from store.
		// 2. Open plaintext DB.
		// 3. Generate new key.
		// 4. Read ALL encrypted data fields.
		// 5. Decrypt using OLD key.
		// 6. Re-encrypt using NEW key.
		// 7. Write re-encrypted data back.
		// 8. Update key in secret store.
		// This needs careful transaction handling.
		return fmt.Errorf("key rotation is not implemented for application-level encryption yet")
	},
}
