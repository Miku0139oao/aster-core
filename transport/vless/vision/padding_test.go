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
