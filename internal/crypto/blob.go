package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidData is returned when the data to be decrypted is invalid
	ErrInvalidData = errors.New("invalid encrypted data")
)

// EncryptBlob encrypts data using AES-GCM with the provided key
// The returned blob format is: nonce (12 bytes) + ciphertext
func EncryptBlob(key, plaintext []byte) ([]byte, error) {
	// Handle empty plaintext case
	if len(plaintext) == 0 {
		plaintext = []byte{} // Ensure it's an empty slice, not nil
	}

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Create nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and seal
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptBlob decrypts data using AES-GCM with the provided key
// The expected blob format is: nonce (12 bytes) + ciphertext
func DecryptBlob(key, ciphertext []byte) ([]byte, error) {
	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Check if ciphertext is long enough
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidData
	}

	// Extract nonce and ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	// Ensure we return an empty slice rather than nil for empty plaintext
	if plaintext == nil {
		plaintext = []byte{}
	}

	return plaintext, nil
}
