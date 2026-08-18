package reality

import "testing"

func TestRealityConnWrapperKeepsCloseWriteProtection(t *testing.T) {
	wrapper := realityConnWrapper{}
	if wrapper.ReaderReplaceable() {
		t.Fatal("REALITY reader must not be replaceable")
	}
	if wrapper.WriterReplaceable() {
		t.Fatal("REALITY writer must not be replaceable")
	}
}
