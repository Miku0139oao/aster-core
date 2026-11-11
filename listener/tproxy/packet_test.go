package tproxy

import (
	"errors"
	"net"
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type packetTestNatTable struct {
	err error
}

func (t *packetTestNatTable) GetOrCreate(C.UDPNatKey, func() C.PacketSender) (C.PacketSender, bool, bool) {
	return nil, false, false
}
func (t *packetTestNatTable) Delete(C.UDPNatKey) {}
func (t *packetTestNatTable) GetOrCreateLocalConn(C.UDPNatKey, string, func() (*net.UDPConn, error)) (*net.UDPConn, error) {
	return nil, t.err
}

func (t *packetTestNatTable) RangeForLocalConn(C.UDPNatKey, func(string, *net.UDPConn) bool) {
}

type packetTestTunnel struct {
	nat C.NatTable
}

func (packetTestTunnel) HandleTCPConn(net.Conn, *C.Metadata)      {}
func (packetTestTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (t packetTestTunnel) NatTable() C.NatTable                   { return t.nat }

func TestCreateOrGetLocalConnPropagatesPromiseError(t *testing.T) {
	expected := errors.New("NAT entry closed")
	tunnel := packetTestTunnel{nat: &packetTestNatTable{err: expected}}
	flow := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345")}

	_, err := createOrGetLocalConn(
		flow,
		netip.MustParseAddrPort("1.1.1.1:53"),
		netip.MustParseAddrPort("192.0.2.1:12345"),
		tunnel,
	)
	require.ErrorIs(t, err, expected)
}

// TestPacketLocalAddrIsStable pins the contract LocalAddr relies on: the
// cached address must equal what net.UDPAddrFromAddrPort would have produced,
// and repeated calls must not allocate or change value. The inbound path asks
// for it four times per datagram (adapter/inbound/packet.go, NewUDPNatKeyForPacket).
func TestPacketLocalAddrIsStable(t *testing.T) {
	for _, addrPort := range []netip.AddrPort{
		netip.MustParseAddrPort("192.0.2.1:12345"),
		netip.MustParseAddrPort("[2001:db8::1]:443"),
	} {
		p := &packet{lAddr: addrPort, lAddrStd: *net.UDPAddrFromAddrPort(addrPort)}

		want := net.UDPAddrFromAddrPort(addrPort)
		first := p.LocalAddr()
		require.Equal(t, want, first)
		require.Same(t, first, p.LocalAddr(), "LocalAddr must not rebuild the address")
		require.Equal(t, addrPort, first.(*net.UDPAddr).AddrPort())
	}
}

// BenchmarkPacketLocalAddrPerDatagram measures the four lookups a single
// datagram performs today. Zero allocs/op is the goal; before the cache this
// path allocated one *net.UDPAddr (plus its IP slice) per call.
func BenchmarkPacketLocalAddrPerDatagram(b *testing.B) {
	p := &packet{lAddr: netip.MustParseAddrPort("192.0.2.1:12345")}
	p.lAddrStd = *net.UDPAddrFromAddrPort(p.lAddr)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.LocalAddr(), p.LocalAddr()
		_, _ = p.LocalAddr(), p.LocalAddr()
	}
}
