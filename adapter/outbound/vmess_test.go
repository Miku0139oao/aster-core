package outbound

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestVmessMKCPBranchDialsUDP covers the mkcp branch of Vmess.dialContext: the
// outbound dials a UDP conn (not TCP) and propagates dial errors.
//
// The added close of rawConn is defensive only. mkcp.Dial's sole error source is
// Config.security(), which only errors when aes.NewCipher rejects the key, and
// the key is always a 16-byte SHA-256 slice, so that branch cannot be forced
// from here. This test pins the surrounding branch so a future refactor keeps
// dialing UDP and keeps returning the raw dial error path.
func TestVmessMKCPBranchDialsUDP(t *testing.T) {
	v, err := NewVmess(VmessOption{
		Name:    "test",
		Server:  "1.2.3.4",
		Port:    443,
		UUID:    "b831381d-6324-4d53-ad4f-8cda48b3081c",
		AlterID: 0,
		Cipher:  "auto",
		Network: "mkcp",
	})
	if err != nil {
		t.Fatalf("NewVmess: %v", err)
	}

	var networks []string
	v.dialer = dialerFunc(func(_ context.Context, network, _ string) (net.Conn, error) {
		networks = append(networks, network)
		return nil, net.ErrClosed
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := v.dialContext(ctx); err == nil {
		t.Fatal("expected the UDP dial error to be propagated")
	}
	if len(networks) != 1 || networks[0] != "udp" {
		t.Fatalf("dialed networks = %v, want [udp]", networks)
	}
}
