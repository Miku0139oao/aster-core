package common

import "testing"

func TestNewQuicTransportSetsConnectionIDLength(t *testing.T) {
	if got := newQuicTransport(nil, 20).ConnectionIDLength; got != 20 {
		t.Fatalf("connection ID length = %d, want 20", got)
	}
	if got := newQuicTransport(nil, 0).ConnectionIDLength; got != 0 {
		t.Fatalf("default connection ID length = %d, want 0", got)
	}
}
