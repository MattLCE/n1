package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptBlob(t *testing.T) {
	// Generate a test key
	key, err := Generate(32)
	require.NoError(t, err, "Failed to generate key")
	require.Len(t, key, 32, "Key should be 32 bytes")

	// Test cases with different plaintext sizes
	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{
			name:      "Empty plaintext",
			plaintext: []byte{},
		},
		{
			name:      "Short plaintext",
			plaintext: []byte("Hello, World!"),
		},
		{
			name:      "Medium plaintext",
			plaintext: bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz"), 10),
		},
		{
			name:      "Long plaintext",
			plaintext: bytes.Repeat([]byte("0123456789"), 1000),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encrypt the plaintext
			ciphertext, err := EncryptBlob(key, tc.plaintext)
			require.NoError(t, err, "Encryption failed")

			// Verify ciphertext is not the same as plaintext (except for empty case)
			if len(tc.plaintext) > 0 {
				assert.NotEqual(t, tc.plaintext, ciphertext, "Ciphertext should differ from plaintext")
			}

			// Decrypt the ciphertext
			decrypted, err := DecryptBlob(key, ciphertext)
			require.NoError(t, err, "Decryption failed")

			// Verify decrypted matches original plaintext
			assert.Equal(t, tc.plaintext, decrypted, "Decrypted data should match original plaintext")
		})
	}
}

func TestDecryptBlobErrors(t *testing.T) {
	// Generate a test key
	key, err := Generate(32)
	require.NoError(t, err, "Failed to generate key")

	// Generate a different key for testing wrong key
	wrongKey, err := Generate(32)
	require.NoError(t, err, "Failed to generate wrong key")

	// Create a valid ciphertext
	plaintext := []byte("Test plaintext")
	ciphertext, err := EncryptBlob(key, plaintext)
	require.NoError(t, err, "Encryption failed")

	// Test cases for decryption errors
	testCases := []struct {
		name       string
		ciphertext []byte
		key        []byte
		expectErr  bool
	}{
		{
			name:       "Too short ciphertext",
			ciphertext: []byte("too short"),
			key:        key,
			expectErr:  true,
		},
		{
			name:       "Wrong key",
			ciphertext: ciphertext,
			key:        wrongKey,
			expectErr:  true,
		},
		{
			name:       "Corrupted ciphertext",
			ciphertext: append(ciphertext[:5], 0xFF, 0xFF, 0xFF),
			key:        key,
			expectErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Attempt to decrypt
			_, err := DecryptBlob(tc.key, tc.ciphertext)

			// Check if error was expected
			if tc.expectErr {
				assert.Error(t, err, "Expected decryption to fail")
			} else {
				assert.NoError(t, err, "Expected decryption to succeed")
			}
		})
	}
}
