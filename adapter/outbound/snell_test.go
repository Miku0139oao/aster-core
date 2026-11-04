package outbound

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
)

// failingConn always fails I/O, so wrapping transports abort immediately.
type failingConn struct {
	closed atomic.Bool
}

func (c *failingConn) Read([]byte) (int, error)         { return 0, io.ErrClosedPipe }
func (c *failingConn) Write([]byte) (int, error)        { return 0, io.ErrClosedPipe }
func (c *failingConn) Close() error                     { c.closed.Store(true); return nil }
func (c *failingConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *failingConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *failingConn) SetDeadline(time.Time) error      { return nil }
func (c *failingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *failingConn) SetWriteDeadline(time.Time) error { return nil }

// TestSnellReusePoolClosesConnOnWrapError covers the snell connection pool
// factory: when the obfs/shadowsocks wrapper fails after a successful dial, the
// dialed connection used to be leaked.
func TestSnellReusePoolClosesConnOnWrapError(t *testing.T) {
	conn := &failingConn{}

	// Snell v2 always uses the connection pool. ShadowTLS with version 1 runs a
	// TLS handshake, which fails instantly against failingConn.
	s, err := NewSnell(SnellOption{
		BasicOption: BasicOption{
			DialerForAPI: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
				return conn, nil
			}),
		},
		Name:     "test",
		Server:   "1.2.3.4",
		Port:     443,
		Psk:      "test",
		Version:  2,
		ObfsOpts: map[string]any{"mode": "shadow-tls", "host": "example.com", "password": "pass", "version": 1},
	})
	if err != nil {
		t.Fatalf("NewSnell: %v", err)
	}
	if !s.reuse {
		t.Fatal("snell version 2 should use the connection pool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = s.DialContext(ctx, &C.Metadata{NetWork: C.TCP, DstIP: netip.MustParseAddr("1.1.1.1"), DstPort: 80})
	if err == nil {
		t.Fatal("expected the ShadowTLS handshake to fail")
	}
	if !conn.closed.Load() {
		t.Fatal("dialed connection was leaked after the wrapper error")
	}
}
