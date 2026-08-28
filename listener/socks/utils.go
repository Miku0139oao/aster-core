package socks

import (
	"encoding/binary"
	"errors"
	"net"

	"github.com/Miku0139oao/aster-core/common/pool"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

var errAddressInvalid = errors.New("address is invalid")

type packet struct {
	pc      net.PacketConn
	rAddr   net.Addr
	payload []byte
	put     func()
}

func (c *packet) Data() []byte {
	return c.payload
}

// WriteBack write UDP packet with source(ip, port) = `addr`
func (c *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	if c.pc == nil {
		return 0, errAddressInvalid
	}
	buf := pool.Get(3 + socks5.MaxAddrLen + len(b))
	defer pool.Put(buf)
	packet, err := appendSocksUDPPacket(buf[:0], addr, b)
	if err != nil {
		return 0, err
	}
	return c.pc.WriteTo(packet, c.rAddr)
}

func appendSocksUDPPacket(dst []byte, addr net.Addr, payload []byte) ([]byte, error) {
	if addr == nil {
		return nil, errAddressInvalid
	}

	dst = append(dst, 0, 0, 0)

	var hostip net.IP
	var port int
	switch a := addr.(type) {
	case *net.UDPAddr:
		hostip = a.IP
		port = a.Port
	case *net.TCPAddr:
		hostip = a.IP
		port = a.Port
	}
	if hostip == nil {
		parsed := socks5.ParseAddr(addr.String())
		if parsed == nil {
			return nil, errAddressInvalid
		}
		dst = append(dst, parsed...)
		return append(dst, payload...), nil
	}

	if ip4 := hostip.To4(); ip4.DefaultMask() != nil {
		dst = append(dst, socks5.AtypIPv4)
		dst = append(dst, ip4...)
		dst = append(dst, byte(uint16(port)>>8), byte(port))
	} else {
		dst = append(dst, socks5.AtypIPv6,
			0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			0, 0)
		copy(dst[len(dst)-net.IPv6len-2:], hostip)
		binary.BigEndian.PutUint16(dst[len(dst)-2:], uint16(port))
	}
	return append(dst, payload...), nil
}

// LocalAddr returns the source IP/Port of UDP Packet
func (c *packet) LocalAddr() net.Addr {
	return c.rAddr
}

func (c *packet) Drop() {
	// Return the inbound payload buffer only. pc/rAddr are the WriteBack handle
	// captured by NAT (WriteBackTarget) and must outlive Drop.
	if c.put != nil {
		c.put()
		c.put = nil
	}
	c.payload = nil
}

func (c *packet) InAddr() net.Addr {
	if c.pc == nil {
		return nil
	}
	return c.pc.LocalAddr()
}
