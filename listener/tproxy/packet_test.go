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
