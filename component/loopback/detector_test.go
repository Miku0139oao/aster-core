package loopback

import (
	"errors"
	"net"
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

type stubAddr struct{ s string }

func (a stubAddr) Network() string { return "tcp" }
func (a stubAddr) String() string  { return a.s }

type stubCConn struct {
	C.Conn
	local net.Addr
}

func (c stubCConn) LocalAddr() net.Addr { return c.local }
func (c stubCConn) Close() error        { return nil }

func TestCheckConnRejectsOnlyRegisteredLocalAddrPort(t *testing.T) {
	detector := NewDetector()
	if detector == nil {
		t.Skip("loopback detector disabled")
	}
	wrapped := detector.NewConn(stubCConn{local: stubAddr{s: "192.168.100.101:54321"}})
	t.Cleanup(func() { _ = wrapped.Close() })

	hit := &C.Metadata{
		SrcIP:   netip.MustParseAddr("192.168.100.101"),
		SrcPort: 54321,
		DstIP:   netip.MustParseAddr("36.155.199.151"),
		DstPort: 6651,
	}
	if err := detector.CheckConn(hit); !errors.Is(err, ErrReject) {
		t.Fatalf("registered outbound AddrPort must reject a recursive dial: %v", err)
	}

	miss := &C.Metadata{
		Type:    C.REDIR,
		SrcIP:   netip.MustParseAddr("192.168.100.101"),
		SrcPort: 55555,
		DstIP:   netip.MustParseAddr("36.155.199.151"),
		DstPort: 6651,
	}
	if err := detector.CheckConn(miss); err != nil {
		t.Fatalf("unregistered captured SYN must not be rejected (that blackholes DIRECT): %v", err)
	}
}

func TestCheckConnAllowsLANClientToPublicDest(t *testing.T) {
	detector := NewDetector()
	if detector == nil {
		t.Skip("loopback detector disabled")
	}
	metadata := &C.Metadata{
		Type:    C.REDIR,
		SrcIP:   netip.MustParseAddr("192.168.1.128"),
		SrcPort: 54321,
		DstIP:   netip.MustParseAddr("36.155.199.151"),
		DstPort: 6651,
	}
	if err := detector.CheckConn(metadata); err != nil {
		t.Fatalf("LAN client to public dest must pass: %v", err)
	}
}
