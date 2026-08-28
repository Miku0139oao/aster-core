package anytls

import (
	"encoding/binary"
	"testing"

	"github.com/Miku0139oao/aster-core/transport/anytls/padding"

	"github.com/stretchr/testify/require"
)

func TestNewAuthenticationPreambleUsesExactPaddingLength(t *testing.T) {
	password := []byte("password")
	const paddingLen = 20000

	b, err := newAuthenticationPreamble(password, paddingLen)
	require.NoError(t, err)
	defer b.Release()

	data := b.Bytes()
	require.Len(t, data, len(password)+2+paddingLen)
	require.Equal(t, password, data[:len(password)])
	require.Equal(t, uint16(paddingLen), binary.BigEndian.Uint16(data[len(password):len(password)+2]))
	require.Equal(t, make([]byte, paddingLen), data[len(password)+2:])
}

func TestNewAuthenticationPreambleRejectsLengthOverflow(t *testing.T) {
	for _, paddingLen := range []int{maxAuthenticationPaddingLength + 1, -1} {
		b, err := newAuthenticationPreamble([]byte("password"), paddingLen)
		require.Error(t, err, paddingLen)
		require.Nil(t, b, paddingLen)
	}
}

func TestInitialPaddingLengthIgnoresCheckMark(t *testing.T) {
	checkMarkFactory := padding.NewPaddingFactory([]byte("stop=1\n0=c"))
	require.NotNil(t, checkMarkFactory)
	require.Equal(t, 0, initialPaddingLength(checkMarkFactory))

	numericFactory := padding.NewPaddingFactory([]byte("stop=1\n0=30-30"))
	require.NotNil(t, numericFactory)
	require.Equal(t, 30, initialPaddingLength(numericFactory))
}
