package pool

import (
	"bytes"
	"testing"
)

func TestPutBufferResetsAndRejectsOversizedCapacity(t *testing.T) {
	large := bytes.NewBuffer(make([]byte, maxPooledBufferCapacity+1))
	PutBuffer(large)
	if large.Len() != 0 {
		t.Fatalf("PutBuffer did not reset oversized buffer: len=%d", large.Len())
	}
	if large.Cap() <= maxPooledBufferCapacity {
		t.Fatalf("test buffer capacity = %d, want > %d", large.Cap(), maxPooledBufferCapacity)
	}

	// The nil guard is part of the pool boundary and must remain harmless.
	PutBuffer(nil)
}

func TestPutBufferKeepsRegularCapacityReusable(t *testing.T) {
	buffer := bytes.NewBuffer(make([]byte, 0, 4096))
	buffer.WriteString("payload")
	PutBuffer(buffer)
	if buffer.Len() != 0 {
		t.Fatalf("PutBuffer did not reset regular buffer: len=%d", buffer.Len())
	}
}
