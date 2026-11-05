package vmess

import (
	"bytes"
	"io"
	"testing"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
)

// metacubex/sing v0.5.8 had a data-loss bug in bufio.ChunkReader: after a small
// read cached a partial chunk, a ReadBuffer with a target at least maxChunkSize
// took the fast path to upstream and returned bytes that skipped the cached
// tail, corrupting the stream. v0.5.9 drains the cache first.
//
// This is the shape sing-vmess uses for VMess AEAD and VLESS Vision, so the
// regression is on aster's default download path, not an edge case.
func TestChunkReaderServesCacheBeforeFastPath(t *testing.T) {
	payload := make([]byte, 40000)
	for i := range payload {
		payload[i] = byte('a' + (i % 26))
	}
	const chunk = 16384
	r := bufio.NewChunkReader(bytes.NewReader(payload), chunk)

	head := make([]byte, 100)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatalf("first read: %v", err)
	}

	large := buf.NewSize(chunk)
	if err := r.ReadBuffer(large); err != nil && err != io.EOF {
		t.Fatalf("large read: %v", err)
	}
	tail := append([]byte(nil), large.Bytes()...)
	large.Release()

	got := append(append([]byte(nil), head...), tail...)
	want := payload[:len(got)]
	if !bytes.Equal(got, want) {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("stream desynchronised at byte %d of %d: cache tail was skipped", i, len(got))
			}
		}
		t.Fatalf("stream length differs: got %d want %d", len(got), len(want))
	}
}
