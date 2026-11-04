package tproxy

import (
	"errors"
	"net"
	"net/netip"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/pool"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
)

type packet struct {
	pc        net.PacketConn
	lAddr     netip.AddrPort
	lAddrStd  net.UDPAddr // cached view of lAddr; LocalAddr returns &lAddrStd
	buf       []byte
	tunnel    C.Tunnel
	additions []inbound.Addition
	natKey    C.UDPNatKey
}

func (c *packet) Data() []byte {
	return c.buf
}

// WriteBack opens a new socket binding `addr` to write UDP packet back
func (c *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	rAddr := addr.(*net.UDPAddr).AddrPort() // tunnel's handleUDPToLocal will ensure addr is *net.UDPAddr
	tc, err := createOrGetLocalConn(c.natKey, rAddr, c.lAddr, c.tunnel, c.additions...)
	if err != nil {
		return
	}
	n, err = tc.Write(b)
	return
}

// LocalAddr returns the source IP/Port of UDP Packet.
//
// The address is built once per packet and cached. It used to call
// net.UDPAddrFromAddrPort on every call, and the caller chain asks four times
// per datagram (adapter/inbound/packet.go:13,15 and
// constant/adapters.go NewUDPNatKeyForPacket), so every datagram allocated
// four equivalent *net.UDPAddr values.
//
// The returned pointer is stable for the lifetime of this packet. All consumers
// only read the address (metadata.RawSrcAddr is json:"-", the rest go through
// SetRemoteAddr), and the packet itself is retained by the tunnel until Drop,
// which is exactly as long as the address is observed. A new packet object is
// created per datagram in handlePacketConn, so the cache cannot leak across
// packets.
func (c *packet) LocalAddr() net.Addr {
	return &c.lAddrStd
}

func (c *packet) Drop() {
	_ = pool.Put(c.buf)
	c.buf = nil
}

func (c *packet) InAddr() net.Addr {
	return c.pc.LocalAddr()
}

// this function listen at rAddr and write to lAddr
// for here, rAddr is the ip/port client want to access
// lAddr is the ip/port client opened
func createOrGetLocalConn(flow C.UDPNatKey, rAddr, lAddr netip.AddrPort, tunnel C.Tunnel, additions ...inbound.Addition) (*net.UDPConn, error) {
	remote := rAddr.String()
	local := lAddr.String()
	return tunnel.NatTable().GetOrCreateLocalConn(flow, remote, func() (*net.UDPConn, error) {
		conn, err := listenLocalConn(rAddr, lAddr, tunnel, additions...)
		if err != nil {
			log.Errorln("listenLocalConn failed with error: %s, packet loss (rAddr[%T]=%s lAddr[%T]=%s)", err.Error(), rAddr, remote, lAddr, local)
		}
		return conn, err
	})
}

// this function listen at rAddr
// and send what received to program itself, then send to real remote
func listenLocalConn(rAddr, lAddr netip.AddrPort, tunnel C.Tunnel, additions ...inbound.Addition) (*net.UDPConn, error) {
	lc, err := dialUDP("udp", rAddr, lAddr)
	if err != nil {
		return nil, err
	}
	go func() {
		log.Debugln("TProxy listenLocalConn rAddr=%s lAddr=%s", rAddr, lAddr)
		for {
			buf := pool.Get(pool.UDPBufferSize)
			br, err := lc.Read(buf)
			if err != nil {
				pool.Put(buf)
				if errors.Is(err, net.ErrClosed) {
					log.Debugln("TProxy local conn listener exit.. rAddr=%s lAddr=%s", rAddr, lAddr)
					return
				}
				continue
			}
			// since following localPackets are pass through this socket which listen rAddr
			// I choose current listener as packet's packet conn
			handlePacketConn(lc, tunnel, buf[:br], lAddr, rAddr, additions...)
		}
	}()
	return lc, nil
}
