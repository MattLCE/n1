//go:build linux

package secretstore

import (
	"fmt" // Added fmt for potential future error wrapping
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func init() { Default = fileStore{} }

type fileStore struct{}

// path calculates the secret file path based on ~/.n1-secrets/<name>
// Note: 'name' is expected to be the absolute vault path here.
func (f fileStore) path(name string) (string, error) { // Added error return
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	// Check if name starts with the vault ID prefix
	if strings.HasPrefix(name, "n1_vault_") {
		// This is a vault ID-based name, not a path, so we don't need to check if it's absolute
		// Just use it as a filename in the .n1-secrets directory
		return filepath.Join(u.HomeDir, ".n1-secrets", name), nil
	}

	// For path-based names, check if the path is absolute
	if !filepath.IsAbs(name) {
		// This wasn't explicitly handled before, but relying on the absolute path
		// being passed seems to be the implicit contract.
		return "", fmt.Errorf("secret name (vault path) must be absolute: %s", name)
	}
	// Original logic joined HomeDir + .n1-secrets + name
	// This could create deeply nested structures like /root/.n1-secrets/test/test/sync/data/vault1/vault.db
	// which might be unexpected. Let's stick to the original implementation for the revert.
	return filepath.Join(u.HomeDir, ".n1-secrets", name), nil
}

func (f fileStore) Put(n string, d []byte) error {
	secretPath, err := f.path(n) // Use path method
	if err != nil {
		return fmt.Errorf("failed to get secret path for '%s': %w", n, err)
	}

	// Ensure the *full* directory path exists
	dirPath := filepath.Dir(secretPath)
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return fmt.Errorf("failed to create secret directory '%s': %w", dirPath, err)
	}

	// Write the file
	if err := os.WriteFile(secretPath, d, 0600); err != nil {
		return fmt.Errorf("failed to write secret file '%s': %w", secretPath, err)
	}
	return nil
}

func (f fileStore) Get(n string) ([]byte, error) {
	secretPath, err := f.path(n) // Use path method
	if err != nil {
		return nil, fmt.Errorf("failed to get secret path for '%s': %w", n, err)
	}
	data, err := os.ReadFile(secretPath)
	if err != nil {
		// Wrap os.ErrNotExist for consistency
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("secret for '%s' not found at '%s': %w", n, secretPath, os.ErrNotExist)
		}
		return nil, fmt.Errorf("failed to read secret file '%s': %w", secretPath, err) // Wrap other errors
	}
	return data, nil
}

func (f fileStore) Delete(n string) error {
	secretPath, err := f.path(n) // Use path method
	if err != nil {
		return fmt.Errorf("failed to get secret path for '%s': %w", n, err)
	}
	err = os.Remove(secretPath)
	if err != nil && !os.IsNotExist(err) { // Ignore not found errors
		return fmt.Errorf("failed to delete secret file '%s': %w", secretPath, err)
	}
	return nil
}
