package miror

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/n1/n1/internal/log"
)

// WALImpl implements the WAL interface using SQLite.
type WALImpl struct {
	db           *sql.DB
	path         string
	mu           sync.Mutex
	bytesWritten int64
	syncInterval int
}

// NewWAL creates a new WAL at the specified path.
func NewWAL(path string, syncInterval int) (*WALImpl, error) {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Open the database
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_sync=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL database: %w", err)
	}

	// Initialize the schema
	if err := initWALSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize WAL schema: %w", err)
	}

	return &WALImpl{
		db:           db,
		path:         path,
		syncInterval: syncInterval,
	}, nil
}

// initWALSchema initializes the WAL database schema.
func initWALSchema(db *sql.DB) error {
	// Create the sessions table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id BLOB PRIMARY KEY,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_active TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// Create the transfers table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS transfers (
			session_id BLOB NOT NULL,
			object_hash BLOB NOT NULL,
			direction TEXT NOT NULL,
			offset INTEGER NOT NULL DEFAULT 0,
			completed BOOLEAN NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (session_id, object_hash),
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}

	// Create an index on the session_id column
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_transfers_session_id ON transfers(session_id)
	`)
	if err != nil {
		return err
	}

	// Create a trigger to update the updated_at column
	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS update_transfers_timestamp
		AFTER UPDATE ON transfers
		BEGIN
			UPDATE transfers SET updated_at = CURRENT_TIMESTAMP WHERE session_id = NEW.session_id AND object_hash = NEW.object_hash;
		END
	`)
	if err != nil {
		return err
	}

	// Create a trigger to update the last_active column in sessions
	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS update_sessions_last_active
		AFTER UPDATE ON transfers
		BEGIN
			UPDATE sessions SET last_active = CURRENT_TIMESTAMP WHERE id = NEW.session_id;
		END
	`)
	return err
}

// LogSend logs a send operation.
func (w *WALImpl) LogSend(sessionID SessionID, objectHash ObjectHash) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure the session exists
	if err := w.ensureSession(sessionID); err != nil {
		return err
	}

	// Insert or replace the transfer record
	_, err := w.db.Exec(
		"INSERT OR REPLACE INTO transfers (session_id, object_hash, direction, offset, completed) VALUES (?, ?, 'send', 0, 0)",
		sessionID[:], objectHash[:],
	)
	if err != nil {
		return fmt.Errorf("failed to log send operation: %w", err)
	}

	// Check if we need to sync
	w.bytesWritten += 32 * 2 // Approximate size of the record
	if w.bytesWritten >= int64(w.syncInterval) {
		if err := w.sync(); err != nil {
			log.Warn().Err(err).Msg("Failed to sync WAL")
		}
	}

	return nil
}

// LogReceive logs a receive operation.
func (w *WALImpl) LogReceive(sessionID SessionID, objectHash ObjectHash) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure the session exists
	if err := w.ensureSession(sessionID); err != nil {
		return err
	}

	// Insert or replace the transfer record
	_, err := w.db.Exec(
		"INSERT OR REPLACE INTO transfers (session_id, object_hash, direction, offset, completed) VALUES (?, ?, 'receive', 0, 0)",
		sessionID[:], objectHash[:],
	)
	if err != nil {
		return fmt.Errorf("failed to log receive operation: %w", err)
	}

	// Check if we need to sync
	w.bytesWritten += 32 * 2 // Approximate size of the record
	if w.bytesWritten >= int64(w.syncInterval) {
		if err := w.sync(); err != nil {
			log.Warn().Err(err).Msg("Failed to sync WAL")
		}
	}

	return nil
}

// LogProgress logs progress of a transfer.
func (w *WALImpl) LogProgress(sessionID SessionID, objectHash ObjectHash, offset int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Update the transfer record
	result, err := w.db.Exec(
		"UPDATE transfers SET offset = ? WHERE session_id = ? AND object_hash = ?",
		offset, sessionID[:], objectHash[:],
	)
	if err != nil {
		return fmt.Errorf("failed to log progress: %w", err)
	}

	// Check if the record exists
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrInvalidSession
	}

	// Check if we need to sync
	w.bytesWritten += 8 // Approximate size of the offset update
	if w.bytesWritten >= int64(w.syncInterval) {
		if err := w.sync(); err != nil {
			log.Warn().Err(err).Msg("Failed to sync WAL")
		}
	}

	return nil
}

// GetProgress gets the progress of a transfer.
func (w *WALImpl) GetProgress(sessionID SessionID, objectHash ObjectHash) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var offset int64
	err := w.db.QueryRow(
		"SELECT offset FROM transfers WHERE session_id = ? AND object_hash = ?",
		sessionID[:], objectHash[:],
	).Scan(&offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidSession
		}
		return 0, fmt.Errorf("failed to get progress: %w", err)
	}

	return offset, nil
}

// CompleteTransfer marks a transfer as complete.
func (w *WALImpl) CompleteTransfer(sessionID SessionID, objectHash ObjectHash) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Update the transfer record
	result, err := w.db.Exec(
		"UPDATE transfers SET completed = 1 WHERE session_id = ? AND object_hash = ?",
		sessionID[:], objectHash[:],
	)
	if err != nil {
		return fmt.Errorf("failed to complete transfer: %w", err)
	}

	// Check if the record exists
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrInvalidSession
	}

	// Check if we need to sync
	w.bytesWritten += 1 // Approximate size of the completed update
	if w.bytesWritten >= int64(w.syncInterval) {
		if err := w.sync(); err != nil {
			log.Warn().Err(err).Msg("Failed to sync WAL")
		}
	}

	return nil
}

// GetSession gets information about a session.
func (w *WALImpl) GetSession(sessionID SessionID) (time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var lastActive time.Time
	err := w.db.QueryRow(
		"SELECT last_active FROM sessions WHERE id = ?",
		sessionID[:],
	).Scan(&lastActive)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrInvalidSession
		}
		return time.Time{}, fmt.Errorf("failed to get session: %w", err)
	}

	return lastActive, nil
}

// CleanupSession removes all entries for a session.
func (w *WALImpl) CleanupSession(sessionID SessionID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Delete the session (cascade will delete transfers)
	result, err := w.db.Exec(
		"DELETE FROM sessions WHERE id = ?",
		sessionID[:],
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup session: %w", err)
	}

	// Check if the record exists
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrInvalidSession
	}

	// Force a sync after cleanup
	if err := w.sync(); err != nil {
		log.Warn().Err(err).Msg("Failed to sync WAL after cleanup")
	}

	return nil
}

// CleanupExpired removes all expired entries.
func (w *WALImpl) CleanupExpired(maxAge time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Calculate the cutoff time
	cutoff := time.Now().Add(-maxAge)

	// Delete expired sessions (cascade will delete transfers)
	_, err := w.db.Exec(
		"DELETE FROM sessions WHERE last_active < ?",
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup expired sessions: %w", err)
	}

	// Force a sync after cleanup
	if err := w.sync(); err != nil {
		log.Warn().Err(err).Msg("Failed to sync WAL after cleanup")
	}

	return nil
}

// Close closes the WAL.
func (w *WALImpl) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Sync before closing
	if err := w.sync(); err != nil {
		log.Warn().Err(err).Msg("Failed to sync WAL before closing")
	}

	// Close the database
	if err := w.db.Close(); err != nil {
		return fmt.Errorf("failed to close WAL database: %w", err)
	}

	return nil
}

// sync syncs the WAL to disk.
func (w *WALImpl) sync() error {
	_, err := w.db.Exec("PRAGMA wal_checkpoint(FULL)")
	if err != nil {
		return fmt.Errorf("failed to checkpoint WAL: %w", err)
	}
	w.bytesWritten = 0
	return nil
}

// ensureSession ensures that a session exists in the database.
func (w *WALImpl) ensureSession(sessionID SessionID) error {
	// Try to insert the session
	_, err := w.db.Exec(
		"INSERT OR IGNORE INTO sessions (id) VALUES (?)",
		sessionID[:],
	)
	if err != nil {
		// Check if it's a constraint violation (session already exists)
		if sqliteErr, ok := err.(sqlite3.Error); ok && sqliteErr.Code == sqlite3.ErrConstraint {
			// Session already exists, update the last_active timestamp
			_, err = w.db.Exec(
				"UPDATE sessions SET last_active = CURRENT_TIMESTAMP WHERE id = ?",
				sessionID[:],
			)
			if err != nil {
				return fmt.Errorf("failed to update session: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to ensure session: %w", err)
	}
	return nil
}
