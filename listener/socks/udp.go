package socks

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/sockopt"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

type UDPListener struct {
	packetConn net.PacketConn
	addr       string
	closed     atomic.Bool
}

// RawAddress implements C.Listener
func (l *UDPListener) RawAddress() string {
	return l.addr
}

// Address implements C.Listener
func (l *UDPListener) Address() string {
	return l.packetConn.LocalAddr().String()
}

// Close implements C.Listener
func (l *UDPListener) Close() error {
	l.closed.Store(true)
	return l.packetConn.Close()
}

func NewUDP(addr string, tunnel C.Tunnel, additions ...inbound.Addition) (*UDPListener, error) {
	return NewUDPWithConfig(defaultConfig(addr), inbound.NewListenConfig(), tunnel, additions...)
}

func NewUDPWithConfig(config LC.AuthServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (*UDPListener, error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-SOCKS"),
			inbound.WithSpecialRules(""),
		}
	}
	l, err := lc.ListenPacket(context.Background(), "udp", config.Listen)
	if err != nil {
		return nil, err
	}

	if err := sockopt.UDPReuseaddr(l); err != nil {
		log.Warnln("Failed to Reuse UDP Address: %s", err)
	}

	sl := &UDPListener{
		packetConn: l,
		addr:       config.Listen,
	}
	conn := N.NewEnhancePacketConn(l)
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
			handleSocksUDP(l, tunnel, data, put, remoteAddr, additions...)
		}
	}()

	return sl, nil
}

func handleSocksUDP(pc net.PacketConn, tunnel C.Tunnel, buf []byte, put func(), addr net.Addr, additions ...inbound.Addition) {
	target, payload, err := socks5.DecodeUDPPacket(buf)
	if err != nil {
		// Unresolved UDP packet, return buffer to the pool
		if put != nil {
			put()
		}
		return
	}
	packet := &packet{
		pc:      pc,
		rAddr:   addr,
		payload: payload,
		put:     put,
	}
	tunnel.HandleUDPPacket(inbound.NewPacket(target, packet, C.SOCKS5, additions...))
}
