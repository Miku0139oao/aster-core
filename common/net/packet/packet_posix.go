//go:build !windows

package packet

import (
	"net"
	"strconv"
	"syscall"

	"github.com/Miku0139oao/aster-core/common/pool"
)

type enhanceUDPConn struct {
	*net.UDPConn
	rawConn syscall.RawConn
}

func (s *udpWaitSlot) rawRead(fd uintptr) (done bool) {
	s.dropBuf()
	readBuf := pool.Get(pool.UDPBufferSize)
	s.buf = readBuf
	var readFrom syscall.Sockaddr
	var readN int
	readN, _, _, readFrom, s.readErr = syscall.Recvmsg(int(fd), readBuf, nil, 0)
	if readN > 0 {
		s.data = readBuf[:readN]
	} else {
		s.dropBuf()
	}
	if s.readErr == syscall.EAGAIN {
		s.dropBuf()
		return false
	}
	if readFrom != nil {
		switch from := readFrom.(type) {
		case *syscall.SockaddrInet4:
			ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 4 bytes
			s.addr = &net.UDPAddr{IP: ip[:], Port: from.Port}
		case *syscall.SockaddrInet6:
			ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 16 bytes
			zone := ""
			if from.ZoneId != 0 {
				zone = strconv.FormatInt(int64(from.ZoneId), 10)
			}
			s.addr = &net.UDPAddr{IP: ip[:], Port: from.Port, Zone: zone}
		}
	}
	return true
}
