package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	key, err := Generate(32)
	require.NoError(t, err)
	require.Len(t, key, 32)
}
