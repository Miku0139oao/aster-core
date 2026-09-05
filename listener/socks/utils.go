package socks

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/pool"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

var errAddressInvalid = errors.New("address is invalid")

type packet struct {
	pc       net.PacketConn
	rAddr    net.Addr
	payload  []byte
	put      func()
	metadata *C.Metadata
	rawDst   net.UDPAddr
	dstIP    [16]byte
}

var packetMetadataPool = sync.Pool{New: func() any { return new(C.Metadata) }}

func acquirePacketMetadata() *C.Metadata {
	metadata := packetMetadataPool.Get().(*C.Metadata)
	metadata.NetWork = C.UDP
	metadata.Type = C.SOCKS5
	return metadata
}

func releasePacketMetadata(metadata *C.Metadata) {
	*metadata = C.Metadata{}
	packetMetadataPool.Put(metadata)
}

// fillSocksUDPMetadata matches inbound.NewPacket field assignment order and
// semantics. target must already have passed socks5.DecodeUDPPacket (SplitAddr
// covers all slicing). Raw destination is stored on the packet; additions run last.
func fillSocksUDPMetadata(pkt *packet, target socks5.Addr, additions ...inbound.Addition) *C.Metadata {
	metadata := acquirePacketMetadata()
	pkt.metadata = metadata
	applySocksUDPDestination(metadata, target)
	metadata.RawSrcAddr = pkt.LocalAddr()
	assignPacketDestination(pkt, metadata)
	applyCopiedAddr(&metadata.SrcIP, &metadata.SrcPort, pkt.LocalAddr())
	if p, ok := any(pkt).(C.UDPPacketInAddr); ok {
		applyCopiedAddr(&metadata.InIP, &metadata.InPort, p.InAddr())
	}
	inbound.ApplyAdditions(metadata, additions...)
	return metadata
}

func applySocksUDPDestination(metadata *C.Metadata, target socks5.Addr) {
	switch target[0] {
	case socks5.AtypDomainName:
		metadata.Host = strings.TrimRight(string(target[2:2+target[1]]), ".")
		metadata.DstPort = uint16((int(target[2+target[1]]) << 8) | int(target[2+target[1]+1]))
	case socks5.AtypIPv4:
		metadata.DstIP, _ = netip.AddrFromSlice(target[1 : 1+net.IPv4len])
		metadata.DstPort = uint16((int(target[1+net.IPv4len]) << 8) | int(target[1+net.IPv4len+1]))
	case socks5.AtypIPv6:
		metadata.DstIP, _ = netip.AddrFromSlice(target[1 : 1+net.IPv6len])
		metadata.DstPort = uint16((int(target[1+net.IPv6len]) << 8) | int(target[1+net.IPv6len+1]))
	}
	metadata.DstIP = metadata.DstIP.Unmap()
}

func assignPacketDestination(pkt *packet, metadata *C.Metadata) {
	if !metadata.DstIP.IsValid() {
		// Match Metadata.UDPAddr(): typed *UDPAddr nil, not a nil interface.
		var none *net.UDPAddr
		metadata.RawDstAddr = none
		return
	}
	ip := metadata.DstIP.Unmap()
	pkt.rawDst.Port = int(metadata.DstPort)
	pkt.rawDst.Zone = ip.Zone()
	switch {
	case ip.Is4():
		ip4 := ip.As4()
		copy(pkt.dstIP[:4], ip4[:])
		pkt.rawDst.IP = pkt.dstIP[:4]
	case ip.Is6():
		ip16 := ip.As16()
		copy(pkt.dstIP[:], ip16[:])
		pkt.rawDst.IP = pkt.dstIP[:]
	default:
		pkt.rawDst.IP = nil
		pkt.rawDst.Zone = ""
		var none *net.UDPAddr
		metadata.RawDstAddr = none
		return
	}
	metadata.RawDstAddr = &pkt.rawDst
}

func applyCopiedAddr(ip *netip.Addr, port *uint16, addr net.Addr) {
	var tmp C.Metadata
	if err := tmp.SetRemoteAddr(addr); err == nil {
		*ip = tmp.DstIP
		*port = tmp.DstPort
	}
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
	// Payload/metadata are one-shot. pc/rAddr are the WriteBack handle captured
	// by NAT (WriteBackTarget) and must outlive Drop. The packet object is not pooled.
	if c.put != nil {
		c.put()
		c.put = nil
	}
	c.payload = nil
	if c.metadata != nil {
		releasePacketMetadata(c.metadata)
		c.metadata = nil
	}
}

func (c *packet) InAddr() net.Addr {
	if c.pc == nil {
		return nil
	}
	return c.pc.LocalAddr()
}
