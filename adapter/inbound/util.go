package inbound

import (
	"net"
	"net/netip"
	"strings"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/socks5"

	"github.com/metacubex/http"
)

func parseSocksAddr(target socks5.Addr) *C.Metadata {
	metadata := &C.Metadata{}

	// A nil or truncated address is not a valid SOCKS request. socks5.ReadAddr
	// always returns a well-formed one, but socks5.ParseAddr yields nil for hosts
	// longer than 255 bytes, so treat a short target as an empty destination
	// instead of panicking on target[0].
	if len(target) < 2 {
		return metadata
	}

	switch target[0] {
	case socks5.AtypDomainName:
		// Convert before arithmetic so long domain lengths cannot overflow a byte.
		domainEnd := 2 + int(target[1])
		if len(target) < domainEnd+2 {
			break
		}
		// trim for FQDN
		metadata.Host = strings.TrimRight(string(target[2:domainEnd]), ".")
		metadata.DstPort = uint16(target[domainEnd])<<8 | uint16(target[domainEnd+1])
	case socks5.AtypIPv4:
		if len(target) < 1+net.IPv4len+2 {
			break
		}
		metadata.DstIP, _ = netip.AddrFromSlice(target[1 : 1+net.IPv4len])
		metadata.DstPort = uint16((int(target[1+net.IPv4len]) << 8) | int(target[1+net.IPv4len+1]))
	case socks5.AtypIPv6:
		if len(target) < 1+net.IPv6len+2 {
			break
		}
		metadata.DstIP, _ = netip.AddrFromSlice(target[1 : 1+net.IPv6len])
		metadata.DstPort = uint16((int(target[1+net.IPv6len]) << 8) | int(target[1+net.IPv6len+1]))
	}
	metadata.DstIP = metadata.DstIP.Unmap()

	return metadata
}

func parseHTTPAddr(request *http.Request) *C.Metadata {
	host := request.URL.Hostname()
	port := request.URL.Port()
	if port == "" {
		port = "80"
	}

	// trim FQDN (#737)
	host = strings.TrimRight(host, ".")

	metadata := &C.Metadata{}
	_ = metadata.SetRemoteAddress(net.JoinHostPort(host, port))
	return metadata
}

func prefixesContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	if len(prefixes) == 0 {
		return false
	}
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap().WithZone("") // netip.Prefix.Contains returns false if ip has an IPv6 zone
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
