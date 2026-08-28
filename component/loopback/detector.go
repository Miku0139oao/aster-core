package loopback

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"

	"github.com/Miku0139oao/aster-core/common/callback"
	"github.com/Miku0139oao/aster-core/common/xsync"
	"github.com/Miku0139oao/aster-core/component/iface"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/constant/features"
)

var disableLoopBackDetector, _ = strconv.ParseBool(os.Getenv("DISABLE_LOOPBACK_DETECTOR"))

func init() {
	if features.CMFA {
		disableLoopBackDetector = true
	}
}

var ErrReject = errors.New("reject loopback connection")

type Detector struct {
	connMap       xsync.Map[netip.AddrPort, struct{}]
	packetConnMap xsync.Map[uint16, struct{}]
}

func NewDetector() *Detector {
	if disableLoopBackDetector {
		return nil
	}
	return &Detector{}
}

func addrPortFromNetAddr(addr net.Addr) (netip.AddrPort, bool) {
	if addr == nil {
		return netip.AddrPort{}, false
	}
	// Match Metadata.SetRemoteAddr: unwrap CustomAddr (DIRECT UDP LocalAddr is a uuid string).
	if raw, ok := addr.(interface{ RawAddr() net.Addr }); ok {
		if inner := raw.RawAddr(); inner != nil && inner != addr {
			if p, ok := addrPortFromNetAddr(inner); ok {
				return p, true
			}
		}
	}
	if ap, ok := addr.(interface{ AddrPort() netip.AddrPort }); ok {
		p := ap.AddrPort()
		if p.IsValid() && p.Port() != 0 {
			return netip.AddrPortFrom(p.Addr().Unmap(), p.Port()), true
		}
	}
	p, err := netip.ParseAddrPort(addr.String())
	if err != nil || !p.IsValid() {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(p.Addr().Unmap(), p.Port()), true
}

func (l *Detector) NewConn(conn C.Conn) C.Conn {
	if l == nil {
		return conn
	}
	connAddr, ok := addrPortFromNetAddr(conn.LocalAddr())
	if !ok {
		return conn
	}
	l.connMap.Store(connAddr, struct{}{})
	return callback.NewCloseCallbackConn(conn, func() {
		l.connMap.Delete(connAddr)
	})
}

func (l *Detector) NewPacketConn(conn C.PacketConn) C.PacketConn {
	if l == nil {
		return conn
	}
	connAddr, ok := addrPortFromNetAddr(conn.LocalAddr())
	if !ok {
		return conn
	}
	port := connAddr.Port()
	l.packetConnMap.Store(port, struct{}{})
	return callback.NewCloseCallbackPacketConn(conn, func() {
		l.packetConnMap.Delete(port)
	})
}

func (l *Detector) CheckConn(metadata *C.Metadata) error {
	if l == nil {
		return nil
	}
	connAddr := metadata.SourceAddrPort()
	if !connAddr.IsValid() {
		return nil
	}
	if _, ok := l.connMap.Load(connAddr); ok {
		return fmt.Errorf("%w to: %s", ErrReject, metadata.RemoteAddress())
	}
	return nil
}

func (l *Detector) CheckPacketConn(metadata *C.Metadata) error {
	if l == nil {
		return nil
	}
	connAddr := metadata.SourceAddrPort()
	if !connAddr.IsValid() {
		return nil
	}

	src := connAddr.Addr()
	if !src.IsLoopback() {
		isLocalIp, err := iface.IsLocalIp(src)
		if err != nil {
			return err
		}
		if !isLocalIp {
			return nil
		}
	}

	if _, ok := l.packetConnMap.Load(connAddr.Port()); ok {
		return fmt.Errorf("%w to: %s", ErrReject, metadata.RemoteAddress())
	}
	return nil
}
