//go:build windows

package packet

import (
	"net"
	"strconv"
	"syscall"

	"github.com/Miku0139oao/aster-core/common/pool"

	"golang.org/x/sys/windows"
)

type enhanceUDPConn struct {
	*net.UDPConn
	rawConn syscall.RawConn
}

func (s *udpWaitSlot) rawRead(fd uintptr) (done bool) {
	if !s.hasData {
		s.hasData = true
		// golang's internal/poll.FD.RawRead will Use a zero-byte read as a way to get notified when this
		// socket is readable if we return false. So the `recvfrom` syscall will not block the system thread.
		return false
	}
	s.dropBuf()
	readBuf := pool.Get(pool.UDPBufferSize)
	s.buf = readBuf
	var readFrom windows.Sockaddr
	var readN int
	readN, readFrom, s.readErr = windows.Recvfrom(windows.Handle(fd), readBuf, 0)
	if readN > 0 {
		s.data = readBuf[:readN]
	} else {
		s.dropBuf()
	}
	if s.readErr == windows.WSAEWOULDBLOCK {
		s.dropBuf()
		return false
	}
	if readFrom != nil {
		switch from := readFrom.(type) {
		case *windows.SockaddrInet4:
			ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 4 bytes
			s.addr = &net.UDPAddr{IP: ip[:], Port: from.Port}
		case *windows.SockaddrInet6:
			ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 16 bytes
			zone := ""
			if from.ZoneId != 0 {
				zone = strconv.FormatInt(int64(from.ZoneId), 10)
			}
			s.addr = &net.UDPAddr{IP: ip[:], Port: from.Port, Zone: zone}
		}
	}
	s.hasData = false
	return true
}
