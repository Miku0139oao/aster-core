package vision

import (
	"testing"

	"github.com/Miku0139oao/aster-core/common/buf"
)

func TestApplyPaddingClearsReusedBackingMemory(t *testing.T) {
	const capacity = 2048
	raw := make([]byte, capacity)
	for i := range raw {
		raw[i] = 0xA5
	}
	buffer := buf.As(raw)
	buffer.Resize(PaddingHeaderLen, 0)

	ApplyPadding(buffer, commandPaddingEnd, nil, true)
	const headerWithoutUUID = 1 + 2 + 2
	padding := buffer.Bytes()[headerWithoutUUID:]
	if len(padding) == 0 {
		t.Fatal("expected TLS padding")
	}
	for i, value := range padding {
		if value != 0 {
			t.Fatalf("padding byte %d retained pooled value %#x", i, value)
		}
	}
}

func resetVisionPayload(buffer *buf.Buffer, payload []byte) {
	buffer.Resize(PaddingHeaderLen, 0)
	if _, err := buffer.Write(payload); err != nil {
		panic(err)
	}
}

func BenchmarkApplyPaddingTLS(b *testing.B) {
	payload := make([]byte, 100)
	buffer := buf.NewSize(2048)
	defer buffer.Release()
	resetVisionPayload(buffer, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetVisionPayload(buffer, payload)
		ApplyPadding(buffer, commandPaddingContinue, nil, true)
	}
}

func BenchmarkReshapeBufferSmall(b *testing.B) {
	payload := make([]byte, 512)
	buffer := buf.NewSize(2048)
	defer buffer.Release()
	resetVisionPayload(buffer, payload)
	vc := &Conn{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vc.ReshapeBuffer(buffer)
	}
}
