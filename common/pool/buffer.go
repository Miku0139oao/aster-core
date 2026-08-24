package pool

import (
	"bytes"
	"sync"
)

const maxPooledBufferCapacity = 128 << 10

var bufferPool = sync.Pool{New: func() any { return &bytes.Buffer{} }}

func GetBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

func PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	buf.Reset()
	// Large provider/config bursts must not pin multi-megabyte backing arrays in
	// a process-wide pool. Regular relay and protocol buffers remain reusable.
	if buf.Cap() > maxPooledBufferCapacity {
		return
	}
	bufferPool.Put(buf)
}
