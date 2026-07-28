package shadowsocks

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/sockopt"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/transport/shadowsocks/core"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

type UDPListener struct {
	packetConn net.PacketConn
	closed     atomic.Bool
	closeOnce  sync.Once
	closeErr   error
}

func NewUDP(addr string, lc C.InboundListenConfig, pickCipher core.Cipher, tunnel C.Tunnel, additions ...inbound.Addition) (*UDPListener, error) {
	l, err := lc.ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return nil, err
	}

	if err := sockopt.UDPReuseaddr(l); err != nil {
		log.Warnln("Failed to Reuse UDP Address: %s", err)
	}

	sl := &UDPListener{packetConn: l}
	conn := pickCipher.PacketConn(N.NewEnhancePacketConn(l))
	go func() {
		for {
			data, put, remoteAddr, err := conn.WaitReadFrom()
			if err != nil {
				if put != nil {
					put()
				}
				if sl.closed.Load() {
					break
				}
				continue
			}
			handleSocksUDP(conn, tunnel, data, put, remoteAddr, additions...)
		}
	}()

	return sl, nil
}

func (l *UDPListener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		l.closeErr = l.packetConn.Close()
	})
	return l.closeErr
}

func (l *UDPListener) LocalAddr() net.Addr {
	return l.packetConn.LocalAddr()
}

func handleSocksUDP(pc net.PacketConn, tunnel C.Tunnel, buf []byte, put func(), addr net.Addr, additions ...inbound.Addition) {
	tgtAddr := socks5.SplitAddr(buf)
	if tgtAddr == nil {
		// Unresolved UDP packet, return buffer to the pool
		if put != nil {
			put()
		}
		return
	}
	target := tgtAddr
	payload := buf[len(tgtAddr):]

	packet := &packet{
		pc:      pc,
		rAddr:   addr,
		payload: payload,
		put:     put,
	}
	tunnel.HandleUDPPacket(inbound.NewPacket(target, packet, C.SHADOWSOCKS, additions...))
}
