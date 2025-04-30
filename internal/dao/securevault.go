package dao

import (
	"database/sql"
	"fmt"

	"github.com/n1/n1/internal/crypto"
)

// SecureVaultDAO wraps VaultDAO with encryption/decryption
type SecureVaultDAO struct {
	dao *VaultDAO
	key []byte
}

// NewSecureVaultDAO creates a new SecureVaultDAO
func NewSecureVaultDAO(db *sql.DB, key []byte) *SecureVaultDAO {
	return &SecureVaultDAO{
		dao: NewVaultDAO(db),
		key: key,
	}
}

// Get retrieves and decrypts a record by key
func (d *SecureVaultDAO) Get(key string) ([]byte, error) {
	record, err := d.dao.Get(key)
	if err != nil {
		return nil, err
	}

	// Decrypt the value
	plaintext, err := crypto.DecryptBlob(d.key, record.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt value for key %s: %w", key, err)
	}

	return plaintext, nil
}

// Put encrypts and stores a record
func (d *SecureVaultDAO) Put(key string, value []byte) error {
	// Encrypt the value
	ciphertext, err := crypto.EncryptBlob(d.key, value)
	if err != nil {
		return fmt.Errorf("failed to encrypt value for key %s: %w", key, err)
	}

	// Store the encrypted value
	return d.dao.Put(key, ciphertext)
}

// Delete removes a record by key
func (d *SecureVaultDAO) Delete(key string) error {
	return d.dao.Delete(key)
}

// List returns all keys in the vault
func (d *SecureVaultDAO) List() ([]string, error) {
	return d.dao.List()
}

// RotateKey re-encrypts all values with a new key
func (d *SecureVaultDAO) RotateKey(newKey []byte) error {
	// Get all keys
	keys, err := d.dao.List()
	if err != nil {
		return fmt.Errorf("failed to list keys for rotation: %w", err)
	}

	// We'll use the existing connection for reading and writing
	// This isn't a true transaction since we're not using tx methods,
	// but it's a good starting point for the implementation

	// Re-encrypt each value
	for _, k := range keys {
		// Get and decrypt with old key
		plaintext, err := d.Get(k)
		if err != nil {
			return fmt.Errorf("failed to get value for key %s during rotation: %w", k, err)
		}

		// Encrypt with new key
		ciphertext, err := crypto.EncryptBlob(newKey, plaintext)
		if err != nil {
			return fmt.Errorf("failed to encrypt value for key %s during rotation: %w", k, err)
		}

		// Update the record directly
		_, err = d.dao.db.Exec(
			"UPDATE vault SET value = ? WHERE key = ?",
			ciphertext, k,
		)
		if err != nil {
			return fmt.Errorf("failed to update value for key %s during rotation: %w", k, err)
		}
	}

	// Update the key in memory
	d.key = newKey

	return nil
}
