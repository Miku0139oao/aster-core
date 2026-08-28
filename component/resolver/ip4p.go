package resolver

import (
	"net"
	"net/netip"
	"strconv"

	"github.com/Miku0139oao/aster-core/log"
)

var ip4PEnable bool

func GetIP4PEnable() bool {
	return ip4PEnable
}

func SetIP4PEnable(enableIP4PConvert bool) {
	ip4PEnable = enableIP4PConvert
}

// kanged from https://github.com/heiher/frp/blob/ip4p/client/ip4p.go

func LookupIP4P(addr netip.Addr, port string) (netip.Addr, string) {
	if !ip4PEnable || !addr.Is6() {
		return addr, port
	}
	// As16 is a stack value; AsSlice() would heap-allocate 16 bytes on every IPv6 dial.
	ip := addr.As16()
	if ip[0] == 0x20 && ip[1] == 0x01 &&
		ip[2] == 0x00 && ip[3] == 0x00 {
		out := netip.AddrFrom4([4]byte{ip[12], ip[13], ip[14], ip[15]})
		port = strconv.Itoa(int(ip[10])<<8 + int(ip[11]))
		if log.Enabled(log.DEBUG) {
			log.Debugln("Convert IP4P address %s to %s", addr, net.JoinHostPort(out.String(), port))
		}
		return out, port
	}
	return addr, port
}
