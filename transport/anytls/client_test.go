package anytls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
)

func TestInitialPaddingLengthIgnoresCheckMark(t *testing.T) {
	checkMarkFactory := padding.NewPaddingFactory([]byte("stop=1\n0=c"))
	require.NotNil(t, checkMarkFactory)
	require.Equal(t, 0, initialPaddingLength(checkMarkFactory))

	numericFactory := padding.NewPaddingFactory([]byte("stop=1\n0=30-30"))
	require.NotNil(t, numericFactory)
	require.Equal(t, 30, initialPaddingLength(numericFactory))
}
