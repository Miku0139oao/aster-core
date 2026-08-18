package outbound

import (
	"testing"
)

func TestNewHysteria2RejectsNegativeHandshakeTimeout(t *testing.T) {
	_, err := NewHysteria2(Hysteria2Option{HandshakeTimeout: -1})
	if err == nil {
		t.Fatal("expected negative handshake timeout to be rejected")
	}
	if got, want := err.Error(), "hysteria2 handshake timeout must be non-negative"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
