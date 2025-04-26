package crypto

import "crypto/rand"

// Generate returns n random bytes.
func Generate(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	return buf, err
}
