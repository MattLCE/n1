package dao

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNotFound is returned when a record is not found
	ErrNotFound = errors.New("record not found")
)

// VaultDAO provides access to the vault table
type VaultDAO struct {
	db *sql.DB
}

// VaultRecord represents a record in the vault table
type VaultRecord struct {
	ID        int64
	Key       string
	Value     []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewVaultDAO creates a new VaultDAO
func NewVaultDAO(db *sql.DB) *VaultDAO {
	return &VaultDAO{db: db}
}

// Get retrieves a record by key
func (d *VaultDAO) Get(key string) (*VaultRecord, error) {
	var record VaultRecord
	err := d.db.QueryRow(
		"SELECT id, key, value, created_at, updated_at FROM vault WHERE key = ?",
		key,
	).Scan(&record.ID, &record.Key, &record.Value, &record.CreatedAt, &record.UpdatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get vault record: %w", err)
	}

	return &record, nil
}

// Put inserts or updates a record
func (d *VaultDAO) Put(key string, value []byte) error {
	// Check if record exists
	_, err := d.Get(key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Insert new record
			_, err = d.db.Exec(
				"INSERT INTO vault (key, value) VALUES (?, ?)",
				key, value,
			)
			if err != nil {
				return fmt.Errorf("failed to insert vault record: %w", err)
			}
			return nil
		}
		return err
	}

	// Update existing record
	_, err = d.db.Exec(
		"UPDATE vault SET value = ? WHERE key = ?",
		value, key,
	)
	if err != nil {
		return fmt.Errorf("failed to update vault record: %w", err)
	}

	return nil
}

// Delete removes a record by key
func (d *VaultDAO) Delete(key string) error {
	result, err := d.db.Exec("DELETE FROM vault WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("failed to delete vault record: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// List returns all keys in the vault
func (d *VaultDAO) List() ([]string, error) {
	rows, err := d.db.Query("SELECT key FROM vault ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("failed to query vault keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan vault key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating vault keys: %w", err)
	}

	return keys, nil
}
