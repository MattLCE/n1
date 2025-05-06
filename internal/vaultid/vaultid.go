// Package vaultid provides functionality for generating and retrieving vault identifiers.
package vaultid

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

const (
	// MetadataTableName is the name of the table that stores vault metadata
	MetadataTableName = "metadata"

	// VaultIDKey is the key used to store the vault UUID in the metadata table
	VaultIDKey = "vault_uuid"

	// SecretNamePrefix is the prefix used for secret names in the secret store
	SecretNamePrefix = "n1_vault_"
)

// GenerateVaultID generates a new UUID for a vault
func GenerateVaultID() string {
	return uuid.New().String()
}

// FormatSecretName formats a secret name using the vault ID
func FormatSecretName(vaultID string) string {
	return SecretNamePrefix + vaultID
}

// GetVaultID retrieves the UUID from a vault file
func GetVaultID(db *sql.DB) (string, error) {
	// Check if the metadata table exists
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", MetadataTableName).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("metadata table does not exist")
		}
		return "", fmt.Errorf("failed to check for metadata table: %w", err)
	}

	// Query the vault UUID from the metadata table
	var vaultID string
	err = db.QueryRow("SELECT value FROM metadata WHERE key=?", VaultIDKey).Scan(&vaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("vault UUID not found in metadata")
		}
		return "", fmt.Errorf("failed to query vault UUID: %w", err)
	}

	return vaultID, nil
}

// EnsureVaultID ensures a vault has a UUID, generating one if needed
func EnsureVaultID(db *sql.DB) (string, error) {
	// Try to get the existing vault ID
	vaultID, err := GetVaultID(db)
	if err == nil {
		// Vault ID already exists
		return vaultID, nil
	}

	// Check if the metadata table exists
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", MetadataTableName).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			// Create the metadata table
			_, err = db.Exec(`
				CREATE TABLE IF NOT EXISTS metadata (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
				)
			`)
			if err != nil {
				return "", fmt.Errorf("failed to create metadata table: %w", err)
			}
		} else {
			return "", fmt.Errorf("failed to check for metadata table: %w", err)
		}
	}

	// Generate a new UUID
	vaultID = GenerateVaultID()

	// Store the UUID in the metadata table
	_, err = db.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", VaultIDKey, vaultID)
	if err != nil {
		return "", fmt.Errorf("failed to store vault UUID: %w", err)
	}

	return vaultID, nil
}

// GetVaultIDFromPath opens the database at the given path and retrieves the vault ID
func GetVaultIDFromPath(vaultPath string) (string, error) {
	// Import the sqlite package here to avoid circular dependencies
	db, err := sql.Open("sqlite3", vaultPath)
	if err != nil {
		return "", fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return GetVaultID(db)
}

// EnsureVaultIDFromPath opens the database at the given path and ensures it has a vault ID
func EnsureVaultIDFromPath(vaultPath string) (string, error) {
	// Import the sqlite package here to avoid circular dependencies
	db, err := sql.Open("sqlite3", vaultPath)
	if err != nil {
		return "", fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return EnsureVaultID(db)
}
