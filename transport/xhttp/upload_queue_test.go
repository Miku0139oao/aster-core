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
