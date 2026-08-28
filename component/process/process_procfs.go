package process

import (
	"bytes"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"unicode"
	"unsafe"
)

func splitCmdline(cmdline []byte) string {
	cmdline = bytes.Trim(cmdline, " ")

	idx := bytes.IndexFunc(cmdline, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})

	if idx == -1 {
		return filepath.Base(string(cmdline))
	}
	return filepath.Base(string(cmdline[:idx]))
}

func isPid(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseHexAddrPort(s string, isV6 bool) (netip.Addr, uint16, error) {
	colon := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return netip.Addr{}, 0, fmt.Errorf("invalid addr:port: %s", s)
	}

	port64, err := strconv.ParseUint(s[colon+1:], 16, 16)
	if err != nil {
		return netip.Addr{}, 0, err
	}

	var addr netip.Addr
	if isV6 {
		addr, err = parseHexIPv6(s[:colon])
	} else {
		addr, err = parseHexIPv4(s[:colon])
	}
	return addr, uint16(port64), err
}

func parseHexIPv4(s string) (netip.Addr, error) {
	if len(s) != 8 {
		return netip.Addr{}, fmt.Errorf("invalid ipv4 hex len: %d", len(s))
	}
	var b [4]byte
	for i := 0; i < 4; i++ {
		v, ok := decodeHexByte(s[i*2], s[i*2+1])
		if !ok {
			return netip.Addr{}, fmt.Errorf("invalid ipv4 hex")
		}
		b[i] = v
	}
	var ip [4]byte
	if littleEndian {
		ip[0], ip[1], ip[2], ip[3] = b[3], b[2], b[1], b[0]
	} else {
		ip = b
	}
	return netip.AddrFrom4(ip), nil
}

func parseHexIPv6(s string) (netip.Addr, error) {
	if len(s) != 32 {
		return netip.Addr{}, fmt.Errorf("invalid ipv6 hex len: %d", len(s))
	}
	var ip [16]byte
	for i := 0; i < 4; i++ {
		var b [4]byte
		for j := 0; j < 4; j++ {
			v, ok := decodeHexByte(s[i*8+j*2], s[i*8+j*2+1])
			if !ok {
				return netip.Addr{}, fmt.Errorf("invalid ipv6 hex")
			}
			b[j] = v
		}
		if littleEndian {
			ip[i*4+0] = b[3]
			ip[i*4+1] = b[2]
			ip[i*4+2] = b[1]
			ip[i*4+3] = b[0]
		} else {
			copy(ip[i*4:(i+1)*4], b[:])
		}
	}
	return netip.AddrFrom16(ip), nil
}

func decodeHexByte(hi, lo byte) (byte, bool) {
	h, ok := fromHex(hi)
	if !ok {
		return 0, false
	}
	l, ok := fromHex(lo)
	if !ok {
		return 0, false
	}
	return (h << 4) | l, true
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

var littleEndian = func() bool {
	x := uint32(0x01020304)
	return *(*byte)(unsafe.Pointer(&x)) == 0x04
}()
