package state

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStateDB_GetChainRange(t *testing.T) {
	s := new(StateDB)
	require.NoError(t, s.Open("", true, false, nil))

	// Set the mark frequency to 4
	s.mm.MarkPower = 2
	s.mm.MarkFreq = 1 << s.mm.MarkPower
	s.mm.MarkMask = s.mm.MarkFreq - 1

	id := []byte(t.Name())
	require.NoError(t, s.mm.SetChainID(id))

	const N = 10
	for i := byte(0); i < N; i++ {
		h := sha256.Sum256([]byte{i})
		s.mm.AddHash(h[:])
	}

	const start, end = 1, 6
	hashes, count, err := s.GetChainRange(id, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(N), count)
	require.Equal(t, end-start, len(hashes))
}