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

type stubCPacketConn struct {
	C.PacketConn
	local net.Addr
}

func (c stubCPacketConn) LocalAddr() net.Addr { return c.local }
func (c stubCPacketConn) Close() error        { return nil }

// customLocalAddr mimics common/net.CustomAddr used by outbound UDP PacketConn.
type customLocalAddr struct {
	display string
	raw     net.Addr
}

func (a customLocalAddr) Network() string   { return "udp" }
func (a customLocalAddr) String() string    { return a.display }
func (a customLocalAddr) RawAddr() net.Addr { return a.raw }

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

func TestNewConnRegistersTCPAddrWithoutMetadata(t *testing.T) {
	detector := NewDetector()
	if detector == nil {
		t.Skip("loopback detector disabled")
	}
	wrapped := detector.NewConn(stubCConn{local: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 8), Port: 4242}})
	t.Cleanup(func() { _ = wrapped.Close() })
	hit := &C.Metadata{
		SrcIP:   netip.MustParseAddr("10.0.0.8"),
		SrcPort: 4242,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	if err := detector.CheckConn(hit); !errors.Is(err, ErrReject) {
		t.Fatalf("TCPAddr registration must reject: %v", err)
	}
}

func TestCheckConnNilDetector(t *testing.T) {
	var detector *Detector
	metadata := &C.Metadata{
		SrcIP:   netip.MustParseAddr("127.0.0.1"),
		SrcPort: 1,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	if err := detector.CheckConn(metadata); err != nil {
		t.Fatal(err)
	}
	if got := detector.NewConn(stubCConn{local: stubAddr{s: "127.0.0.1:1"}}); got == nil {
		t.Fatal("nil detector must return conn")
	}
}

func TestNewPacketConnRegistersCustomAddrViaRawAddr(t *testing.T) {
	detector := NewDetector()
	if detector == nil {
		t.Skip("loopback detector disabled")
	}
	raw := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}
	wrapped := detector.NewPacketConn(stubCPacketConn{
		local: customLocalAddr{display: "DIRECT#conn-id-not-an-ip", raw: raw},
	})
	t.Cleanup(func() { _ = wrapped.Close() })

	hit := &C.Metadata{
		SrcIP:   netip.MustParseAddr("127.0.0.1"),
		SrcPort: 9999,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	if err := detector.CheckPacketConn(hit); !errors.Is(err, ErrReject) {
		t.Fatalf("CustomAddr UDP LocalAddr must register via RawAddr: %v", err)
	}
}

func TestNewPacketConnIgnoresDisplayStringWithoutRawAddr(t *testing.T) {
	detector := NewDetector()
	if detector == nil {
		t.Skip("loopback detector disabled")
	}
	wrapped := detector.NewPacketConn(stubCPacketConn{local: stubAddr{s: "DIRECT#not-an-ip"}})
	t.Cleanup(func() { _ = wrapped.Close() })
	hit := &C.Metadata{
		SrcIP:   netip.MustParseAddr("127.0.0.1"),
		SrcPort: 9999,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	if err := detector.CheckPacketConn(hit); err != nil {
		t.Fatalf("unparseable LocalAddr must not register a port: %v", err)
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
