package xhttp

import (
	"sync"
	"testing"
)

func BenchmarkPacketUpWriterWrite(b *testing.B) {
	w := &PacketUpWriter{
		scMaxEachPostBytes:   1 << 20,
		scMinPostsIntervalMs: Range{Min: 1 << 30, Max: 1 << 30},
		buf:                  make([]byte, 0, 1<<20),
	}
	w.writeCond = sync.Cond{L: &w.writeMu}
	payload := make([]byte, 1024)

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.buf = w.buf[:0]
		if _, err := w.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
