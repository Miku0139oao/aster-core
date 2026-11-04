package inbound

import (
	"fmt"
	"strings"
	"testing"

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

// TestParseSocksAddrDomainLengths guards against the uint8 arithmetic overflow
// that made 2+target[1] wrap for long domains and slice out of bounds.
func TestParseSocksAddrDomainLengths(t *testing.T) {
	for length := 0; length <= 255; length++ {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			host := strings.Repeat("a", length)
			got := parseSocksAddr(socksDomainAddr(host, 443))
			if got.Host != host || got.DstPort != 443 {
				t.Fatalf("length %d: host=%q (len %d) port=%d", length, got.Host, len(got.Host), got.DstPort)
			}
		})
	}
}

// TestParseSocksAddrDomainLengthsBoundaryPorts asserts that ports encoded after
// the maximum-length domain are decoded from the correct bytes.
func TestParseSocksAddrDomainLengthsBoundaryPorts(t *testing.T) {
	for _, length := range []int{0, 1, 252, 253, 254, 255} {
		for _, port := range []uint16{0, 1, 0x1234, 443, 65535} {
			t.Run(fmt.Sprintf("len%d/port%d", length, port), func(t *testing.T) {
				host := strings.Repeat("z", length)
				got := parseSocksAddr(socksDomainAddr(host, port))
				if got.Host != host || got.DstPort != port {
					t.Fatalf("host=%q (len %d) port=%d, want port=%d", got.Host, len(got.Host), got.DstPort, port)
				}
			})
		}
	}
}

func TestParseSocksAddrFQDN(t *testing.T) {
	got := parseSocksAddr(socksDomainAddr("example.com.", 443))
	if got.Host != "example.com" || got.DstPort != 443 {
		t.Fatalf("host=%q port=%d", got.Host, got.DstPort)
	}
}

func TestParseSocksAddrFQDNMaxLength(t *testing.T) {
	// 254 bytes plus a trailing dot is the longest FQDN that still fits.
	host := strings.Repeat("a", 254) + "."
	got := parseSocksAddr(socksDomainAddr(host, 443))
	if got.Host != strings.Repeat("a", 254) || got.DstPort != 443 {
		t.Fatalf("host len=%d port=%d", len(got.Host), got.DstPort)
	}
}

func TestParseSocksAddrIPv4(t *testing.T) {
	addr := socks5.Addr{socks5.AtypIPv4, 1, 2, 3, 4, 0x01, 0xbb}
	got := parseSocksAddr(addr)
	if !got.DstIP.IsValid() || got.DstIP.String() != "1.2.3.4" || got.DstPort != 443 {
		t.Fatalf("ip=%s port=%d", got.DstIP, got.DstPort)
	}
}

// TestParseSocksAddrShortTarget ensures malformed short targets cannot panic.
// socks5.ParseAddr returns nil for hosts longer than 255 bytes, so callers can
// legitimately reach parseSocksAddr with a nil or truncated target.
func TestParseSocksAddrShortTarget(t *testing.T) {
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
			got := parseSocksAddr(addr)
			if got == nil {
				t.Fatal("nil metadata")
			}
			if got.Host != "" || got.DstPort != 0 || got.DstIP.IsValid() {
				t.Fatalf("unexpected metadata: host=%q port=%d ip=%s", got.Host, got.DstPort, got.DstIP)
			}
		})
	}
}
