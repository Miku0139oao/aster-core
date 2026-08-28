package net

import (
	"bytes"
	"errors"
	"io"
	"math/bits"
	"testing"

	"github.com/metacubex/sing/common/network"
)

type countedWriter struct {
	buffer *bytes.Buffer
	count  *int64
}

func (w *countedWriter) Write(payload []byte) (int, error) {
	return w.buffer.Write(payload)
}

func (w *countedWriter) UnwrapWriter() (io.Writer, []network.CountFunc) {
	return w.buffer, []network.CountFunc{func(n int64) { *w.count += n }}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

type writeError struct {
	written int
	err     error
}

func (w writeError) Write([]byte) (int, error) {
	return w.written, w.err
}

type countedWriteError struct {
	writer writeError
	count  *int64
}

func (w *countedWriteError) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *countedWriteError) UnwrapWriter() (io.Writer, []network.CountFunc) {
	return w.writer, []network.CountFunc{func(n int64) { *w.count += n }}
}

func TestCopyConnPlainStreamsAndCounters(t *testing.T) {
	payload := bytes.Repeat([]byte("aster"), 10_000)
	var destination bytes.Buffer
	var counted int64
	n, err := copyConn(&countedWriter{buffer: &destination, count: &counted}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || counted != n {
		t.Fatalf("copied=%d counted=%d want=%d", n, counted, len(payload))
	}
	if !bytes.Equal(destination.Bytes(), payload) {
		t.Fatal("copied payload differs")
	}
}

func TestCopyConnDetectsShortWrite(t *testing.T) {
	_, err := copyConn(shortWriter{}, bytes.NewReader([]byte("payload")))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestCopyConnReportsBytesWrittenWithWriteError(t *testing.T) {
	payload := bytes.Repeat([]byte("aster"), 10_000)
	writeErr := errors.New("write failed after progress")

	for _, test := range []struct {
		name    string
		written int
	}{
		{name: "full write", written: len(payload)},
		{name: "partial write", written: len(payload) / 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var counted int64
			n, err := copyConn(&countedWriteError{
				writer: writeError{written: test.written, err: writeErr},
				count:  &counted,
			}, bytes.NewReader(payload))
			if !errors.Is(err, writeErr) {
				t.Fatalf("error = %v, want %v", err, writeErr)
			}
			if n != int64(test.written) || counted != n {
				t.Fatalf("copied=%d counted=%d want=%d", n, counted, test.written)
			}
		})
	}
}

func maskWebSocketReference(key uint32, b []byte) uint32 {
	for i := range b {
		b[i] ^= byte(key)
		key = bits.RotateLeft32(key, -8)
	}
	return key
}

func TestMaskWebSocketMatchesReference(t *testing.T) {
	keys := []uint32{0, 1, 0x12345678, 0xffffffff, 0x01020304}
	lengths := []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 31, 32, 33, 63, 64, 65, 127, 128, 129, 1000, 1024, 4095, 4096}
	offsets := []int{0, 1, 6, 7, 14}
	for _, key := range keys {
		for _, length := range lengths {
			for _, offset := range offsets {
				buf := make([]byte, offset+length+1)
				for i := range buf {
					buf[i] = byte(i * 17)
				}
				want := append([]byte(nil), buf...)
				got := append([]byte(nil), buf...)
				wantKey := maskWebSocketReference(key, want[offset:offset+length])
				gotKey := MaskWebSocket(key, got[offset:offset+length])
				if gotKey != wantKey {
					t.Fatalf("key=0x%x len=%d off=%d returned %#x want %#x", key, length, offset, gotKey, wantKey)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("key=0x%x len=%d off=%d payload mismatch", key, length, offset)
				}
			}
		}
	}
}

func BenchmarkMaskWebSocket(b *testing.B) {
	// 14-byte offset matches websocketConn.WriteBuffer FrontHeadroom.
	for _, size := range []int{1024, 16 * 1024, 32 * 1024} {
		name := "1KiB"
		if size == 16*1024 {
			name = "16KiB"
		} else if size == 32*1024 {
			name = "32KiB"
		}
		b.Run(name, func(b *testing.B) {
			buf := make([]byte, size+14)
			payload := buf[14:]
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				MaskWebSocket(0x12345678, payload)
			}
		})
	}
}
