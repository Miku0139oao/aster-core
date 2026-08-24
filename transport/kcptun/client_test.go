package kcptun

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestClientCloseRejectsFutureStreamsAndIsIdempotent(t *testing.T) {
	client := NewClient(Config{})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	dialed := false
	_, err := client.OpenStream(context.Background(), func(context.Context) (net.PacketConn, net.Addr, error) {
		dialed = true
		return nil, nil, errors.New("must not dial")
	})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("OpenStream error = %v, want net.ErrClosed", err)
	}
	if dialed {
		t.Fatal("closed kcptun client attempted to create a session")
	}
}
