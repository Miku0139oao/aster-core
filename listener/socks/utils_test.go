package socks

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

// socksDomainAddr builds a domain ATYP address with an explicit length byte.
func socksDomainAddr(host string, port uint16) socks5.Addr {
	addr := make(socks5.Addr, 2+len(host)+2)
	addr[0] = socks5.AtypDomainName
	addr[1] = byte(len(host))
	copy(addr[2:], host)
	addr[len(addr)-2] = byte(port >> 8)
	addr[len(addr)-1] = byte(port)
	return addr
}

// TestApplySocksUDPDestinationDomainLengths guards against the uint8 arithmetic
// overflow that made 2+target[1] wrap for long domains and slice out of bounds.
func TestApplySocksUDPDestinationDomainLengths(t *testing.T) {
	for length := 0; length <= 255; length++ {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			host := strings.Repeat("a", length)
			metadata := &C.Metadata{}
			applySocksUDPDestination(metadata, socksDomainAddr(host, 443))
			if metadata.Host != host || metadata.DstPort != 443 {
				t.Fatalf("length %d: host=%q (len %d) port=%d", length, metadata.Host, len(metadata.Host), metadata.DstPort)
			}
		})
	}
}

func TestApplySocksUDPDestinationBoundaryPorts(t *testing.T) {
	for _, length := range []int{0, 1, 252, 253, 254, 255} {
		for _, port := range []uint16{0, 1, 0x1234, 443, 65535} {
			t.Run(fmt.Sprintf("len%d/port%d", length, port), func(t *testing.T) {
				host := strings.Repeat("z", length)
				metadata := &C.Metadata{}
				applySocksUDPDestination(metadata, socksDomainAddr(host, port))
				if metadata.Host != host || metadata.DstPort != port {
					t.Fatalf("host=%q (len %d) port=%d, want port=%d", metadata.Host, len(metadata.Host), metadata.DstPort, port)
				}
			})
		}
	}
}

func TestApplySocksUDPDestinationFQDN(t *testing.T) {
	metadata := &C.Metadata{}
	applySocksUDPDestination(metadata, socksDomainAddr("example.com.", 443))
	if metadata.Host != "example.com" || metadata.DstPort != 443 {
		t.Fatalf("host=%q port=%d", metadata.Host, metadata.DstPort)
	}
}

func TestApplySocksUDPDestinationShortTarget(t *testing.T) {
	cases := map[string]socks5.Addr{
		"nil":            nil,
		"empty":          {},
		"atyp-only":      {socks5.AtypDomainName},
		"truncated-host": {socks5.AtypDomainName, 10, 'a', 'b'},
		"missing-port":   {socks5.AtypDomainName, 2, 'a', 'b', 0x01},
		"short-ipv4":     {socks5.AtypIPv4, 1, 2},
		"short-ipv6":     {socks5.AtypIPv6, 1, 2, 3, 4},
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			metadata := &C.Metadata{}
			applySocksUDPDestination(metadata, addr)
			if metadata.Host != "" || metadata.DstPort != 0 || metadata.DstIP.IsValid() {
				t.Fatalf("unexpected metadata: host=%q port=%d ip=%s", metadata.Host, metadata.DstPort, metadata.DstIP)
			}
		})
	}
}

// TestHandleSocksUDPMaxDomainLength exercises the full packet path with a
// 255-byte domain, which used to panic inside applySocksUDPDestination.
func TestHandleSocksUDPMaxDomainLength(t *testing.T) {
	for _, length := range []int{253, 254, 255} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			host := strings.Repeat("a", length)
			target := socksDomainAddr(host, 443)
			buf, err := socks5.EncodeUDPPacket(target, []byte("payload"))
			if err != nil {
				t.Fatal(err)
			}

			tunnel := &maxDomainCaptureTunnel{}
			var putCalls int
			handleSocksUDP(maxDomainPacketConn{}, tunnel, buf, func() { putCalls++ },
				&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345})

			if putCalls != 0 {
				t.Fatalf("put calls = %d, want 0 for a decoded packet", putCalls)
			}
			if tunnel.metadata == nil {
				t.Fatal("metadata not delivered")
			}
			defer tunnel.packet.Drop()
			if tunnel.metadata.Host != host || tunnel.metadata.DstPort != 443 {
				t.Fatalf("host len=%d port=%d, want len=%d port=443",
					len(tunnel.metadata.Host), tunnel.metadata.DstPort, length)
			}
		})
	}
}

type maxDomainCaptureTunnel struct {
	packet   C.UDPPacket
	metadata *C.Metadata
}

func (t *maxDomainCaptureTunnel) HandleTCPConn(net.Conn, *C.Metadata) {}

func (t *maxDomainCaptureTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	t.packet = packet
	t.metadata = metadata
}

func (t *maxDomainCaptureTunnel) NatTable() C.NatTable { return nil }

type maxDomainPacketConn struct{}

func (maxDomainPacketConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, io.EOF }
func (maxDomainPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return len(b), nil
}
func (maxDomainPacketConn) Close() error                     { return nil }
func (maxDomainPacketConn) LocalAddr() net.Addr              { return nil }
func (maxDomainPacketConn) SetDeadline(time.Time) error      { return nil }
func (maxDomainPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (maxDomainPacketConn) SetWriteDeadline(time.Time) error { return nil }
