package xhttp

import (
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadQueueRejectsPacketOverflowWithoutBlocking(t *testing.T) {
	q := NewUploadQueue(2)
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte{'1'}}))
	require.NoError(t, q.Push(Packet{Seq: 2, Payload: []byte{'2'}}))

	done := make(chan error, 1)
	go func() { done <- q.Push(Packet{Seq: 3, Payload: []byte{'3'}}) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrQueueTooLarge)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("overflowing producer blocked instead of failing fast")
	}

	buf := make([]byte, 4)
	n, err := q.Read(buf)
	require.Zero(t, n)
	require.ErrorIs(t, err, ErrQueueTooLarge)
	require.NoError(t, q.Close())
}

func TestUploadQueueAllowsDuplicateReplacementAtPacketLimit(t *testing.T) {
	q := NewUploadQueue(2, 4)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("aa")}))
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte("bb")}))
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte("b")}))
	require.ErrorIs(t, q.Push(Packet{Seq: 2, Payload: []byte("c")}), ErrQueueTooLarge)

	buf := make([]byte, 4)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "aa", string(buf[:n]))
	n, err = q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "b", string(buf[:n]))
	require.NoError(t, q.Close())
}

func TestUploadQueueByteLimitAndCloseReleaseReferences(t *testing.T) {
	q := NewUploadQueue(4, 3)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("abc")}))
	require.ErrorIs(t, q.Push(Packet{Seq: 1, Payload: []byte("d")}), ErrQueueTooLarge)
	require.NoError(t, q.Close())
	require.Nil(t, q.packets)
	require.Nil(t, q.buf)
	require.False(t, q.hasReady)
	require.Zero(t, q.queuedBytes)
	require.True(t, errors.Is(q.CanPush(2), io.ErrClosedPipe))
}

func TestUploadQueueReassemblesOutOfOrder(t *testing.T) {
	q := NewUploadQueue(8, 64)
	require.NoError(t, q.Push(Packet{Seq: 2, Payload: []byte("c")}))
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("a")}))
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte("b")}))

	buf := make([]byte, 8)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "a", string(buf[:n]))
	n, err = q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "b", string(buf[:n]))
	n, err = q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "c", string(buf[:n]))
	require.NoError(t, q.Close())
}

func TestUploadQueueReplacesInOrderPacket(t *testing.T) {
	q := NewUploadQueue(2, 8)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("aa")}))
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("b")}))

	buf := make([]byte, 8)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "b", string(buf[:n]))
	require.NoError(t, q.Close())
}

func TestUploadQueueSequentialLeavesPacketsNil(t *testing.T) {
	q := NewUploadQueue(8, 1<<20)
	require.Nil(t, q.packets)

	payload := make([]byte, 1024, 1<<20)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: payload}))
	require.Nil(t, q.packets)

	out := make([]byte, 1024)
	n, err := q.Read(out)
	require.NoError(t, err)
	require.Equal(t, 1024, n)
	require.Nil(t, q.buf)
	require.Nil(t, q.packets)
	require.Equal(t, uint64(1), q.nextSeq)
	require.False(t, q.hasReady)
	require.NoError(t, q.Close())
}

func TestUploadQueueDropsEmptyLargeCapPayload(t *testing.T) {
	q := NewUploadQueue(4, 1<<20)
	empty := make([]byte, 0, 1<<20)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: empty}))
	require.Nil(t, q.packets)
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte("x")}))

	buf := make([]byte, 8)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "x", string(buf[:n]))
	require.Nil(t, q.buf)
	require.Nil(t, q.ready)
	require.False(t, q.hasReady)
	require.Equal(t, uint64(2), q.nextSeq)
	require.NoError(t, q.Close())
}

func TestUploadQueuePartialReadKeepsRemainderThenDrops(t *testing.T) {
	q := NewUploadQueue(2, 64)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("abcd")}))

	buf := make([]byte, 2)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "ab", string(buf[:n]))
	require.Equal(t, []byte("cd"), q.buf)

	n, err = q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "cd", string(buf[:n]))
	require.Nil(t, q.buf)
	require.Equal(t, uint64(1), q.nextSeq)
	require.NoError(t, q.Close())
}

func TestUploadQueueRetainsPacketsMapAfterOOODrain(t *testing.T) {
	q := NewUploadQueue(8, 64)
	require.Nil(t, q.packets)
	require.NoError(t, q.Push(Packet{Seq: 2, Payload: []byte("c")}))
	require.NotNil(t, q.packets)

	empty := make([]byte, 0, 1024)
	require.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte("a")}))
	require.NoError(t, q.Push(Packet{Seq: 1, Payload: empty}))

	buf := make([]byte, 8)
	n, err := q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "a", string(buf[:n]))

	n, err = q.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "c", string(buf[:n]))
	require.Nil(t, q.buf)
	require.NotNil(t, q.packets)
	require.Empty(t, q.packets)
	require.Equal(t, uint64(3), q.nextSeq)
	require.NoError(t, q.Close())
}

func BenchmarkUploadQueueSequential(b *testing.B) {
	for _, size := range []int{1024, 16 * 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			q := NewUploadQueue(32, 1<<20)
			payload := make([]byte, size)
			buf := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := q.Push(Packet{Seq: uint64(i), Payload: payload}); err != nil {
					b.Fatal(err)
				}
				n, err := q.Read(buf)
				if err != nil {
					b.Fatal(err)
				}
				if n != size {
					b.Fatalf("short read: %d", n)
				}
			}
		})
	}
}
