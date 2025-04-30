package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBosrCLI performs an integration test of the bosr CLI binary
func TestBosrCLI(t *testing.T) {
	// Skip if not running in CI environment
	if os.Getenv("CI") != "true" {
		t.Skip("Skipping integration test outside of CI environment")
	}

	// Find the bosr binary
	bosrPath := filepath.Join("..", "bin", "bosr")
	if _, err := os.Stat(bosrPath); os.IsNotExist(err) {
		// Try to build it
		buildCmd := exec.Command("go", "build", "-o", bosrPath, "../cmd/bosr")
		output, err := buildCmd.CombinedOutput()
		require.NoError(t, err, "Failed to build bosr binary: %s", output)
	}

	// Create a temporary directory for the test vault
	tmpDir := t.TempDir()
	vaultPath := filepath.Join(tmpDir, "test_vault.db")

	// Test cases to run in sequence
	testCases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, output []byte)
	}{
		{
			name:    "Init vault",
			args:    []string{"init", vaultPath},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Contains(t, string(output), "initialized", "Init output should indicate success")
			},
		},
		{
			name:    "Open vault",
			args:    []string{"open", vaultPath},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Contains(t, string(output), "accessible", "Open output should indicate success")
			},
		},
		{
			name:    "Put value",
			args:    []string{"put", vaultPath, "test_key", "test_value"},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Contains(t, string(output), "stored", "Put output should indicate success")
			},
		},
		{
			name:    "Get value",
			args:    []string{"get", vaultPath, "test_key"},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Equal(t, "test_value\n", string(output), "Get output should be the stored value")
			},
		},
		{
			name:    "Key rotate dry-run",
			args:    []string{"key", "rotate", "--dry-run", vaultPath},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Contains(t, string(output), "Dry run completed", "Dry run output should indicate no changes")
			},
		},
		{
			name:    "Key rotate",
			args:    []string{"key", "rotate", vaultPath},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Contains(t, string(output), "completed", "Rotate output should indicate success")
			},
		},
		{
			name:    "Get value after rotation",
			args:    []string{"get", vaultPath, "test_key"},
			wantErr: false,
			check: func(t *testing.T, output []byte) {
				assert.Equal(t, "test_value\n", string(output), "Get output after rotation should still be the stored value")
			},
		},
	}

	// Run the test cases in sequence
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bosrPath, tc.args...)
			output, err := cmd.CombinedOutput()

			if tc.wantErr {
				assert.Error(t, err, "Expected error but got none")
			} else {
				assert.NoError(t, err, "Unexpected error: %v\nOutput: %s", err, output)
			}

			if tc.check != nil {
				tc.check(t, output)
			}
		})
	}
}
