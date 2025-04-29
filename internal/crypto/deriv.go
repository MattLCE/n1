package crypto

import (
	"crypto/sha256"
	"golang.org/x/crypto/hkdf"
	"io"
)

// DeriveHKDF derives len bytes from a master key with a context string.
func DeriveHKDF(master []byte, context string, n int) ([]byte, error) {
	r := hkdf.New(sha256.New, master, nil, []byte(context))
	out := make([]byte, n)
	_, err := io.ReadFull(r, out)
	return out, err
}
