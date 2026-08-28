package padding

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateRecordPayloadSizes(t *testing.T) {
	factory := NewPaddingFactory([]byte("stop=3\n0=30-30\n1=20-10,c,bad,0-3"))
	require.NotNil(t, factory)
	require.Equal(t, uint32(3), factory.Stop)
	require.Equal(t, []int{30}, factory.GenerateRecordPayloadSizes(0))

	for i := 0; i < 100; i++ {
		sizes := factory.GenerateRecordPayloadSizes(1)
		require.Len(t, sizes, 2)
		require.GreaterOrEqual(t, sizes[0], 10)
		require.Less(t, sizes[0], 20)
		require.Equal(t, CheckMark, sizes[1])
	}
	require.Empty(t, factory.GenerateRecordPayloadSizes(2))
}

func TestGenerateRecordPayloadSizesCallerOwnsSlice(t *testing.T) {
	factory := NewPaddingFactory([]byte("stop=1\n0=30-30"))
	require.NotNil(t, factory)

	first := factory.GenerateRecordPayloadSizes(0)
	require.Equal(t, []int{30}, first)
	first[0] = 7

	second := factory.GenerateRecordPayloadSizes(0)
	require.Equal(t, []int{30}, second)
	require.Equal(t, 7, first[0])
}

func TestNewPaddingFactoryRejectsInvalidWireValues(t *testing.T) {
	for _, scheme := range []string{
		"stop=-1\n0=30-30",
		"stop=4294967296\n0=30-30",
		"stop=1\n0=65536-65536",
	} {
		require.Nil(t, NewPaddingFactory([]byte(scheme)))
	}
}

func BenchmarkGenerateRecordPayloadSizes(b *testing.B) {
	factory := NewPaddingFactory(DefaultPaddingScheme)
	for _, pkt := range []uint32{0, 2} {
		b.Run(strconv.FormatUint(uint64(pkt), 10), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = factory.GenerateRecordPayloadSizes(pkt)
			}
		})
	}
}
