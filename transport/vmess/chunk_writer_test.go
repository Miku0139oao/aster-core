package vmess

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"testing"
)

type capturingWriteCloser struct {
	writes [][]byte
}

func (c *capturingWriteCloser) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (c *capturingWriteCloser) Close() error { return nil }

func chunkWriterPattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func TestChunkWriterCapturingRoundTrip(t *testing.T) {
	sizes := []int{0, 1, chunkSize, chunkSize + 1, chunkSize + 7, chunkSize * 2}
	for _, size := range sizes {
		size := size
		t.Run(fmt.Sprintf("n=%d", size), func(t *testing.T) {
			payload := chunkWriterPattern(size)
			orig := append([]byte(nil), payload...)
			capw := &capturingWriteCloser{}
			n, err := newChunkWriter(capw).Write(payload)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			if n != size {
				t.Fatalf("n=%d want=%d", n, size)
			}
			if !bytes.Equal(payload, orig) {
				t.Fatal("caller payload mutated")
			}

			var wire bytes.Buffer
			remaining := size
			for i, rec := range capw.writes {
				if len(rec) < lenSize {
					t.Fatalf("write %d too short: %d", i, len(rec))
				}
				gotLen := int(binary.BigEndian.Uint16(rec[:lenSize]))
				if len(rec) != lenSize+gotLen {
					t.Fatalf("write %d wire=%d prefix=%d", i, len(rec), gotLen)
				}
				wantChunk := chunkSize
				if remaining < chunkSize {
					wantChunk = remaining
				}
				if gotLen != wantChunk {
					t.Fatalf("write %d chunk=%d want=%d remaining=%d", i, gotLen, wantChunk, remaining)
				}
				if !bytes.Equal(rec[lenSize:], orig[size-remaining:size-remaining+gotLen]) {
					t.Fatalf("write %d payload mismatch", i)
				}
				wire.Write(rec)
				remaining -= gotLen
			}
			if remaining != 0 {
				t.Fatalf("unconsumed payload %d across %d writes", remaining, len(capw.writes))
			}
			if size == 0 {
				if len(capw.writes) != 0 {
					t.Fatalf("empty write produced %d records", len(capw.writes))
				}
				return
			}

			got := make([]byte, size)
			if _, err := io.ReadFull(newChunkReader(&wire), got); err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got, orig) {
				t.Fatal("roundtrip mismatch")
			}
		})
	}
}
