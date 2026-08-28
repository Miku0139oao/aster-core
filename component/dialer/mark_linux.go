//go:build linux

package dialer

import (
	"context"
	"net"
	"net/netip"
	"syscall"

	"github.com/Miku0139oao/aster-core/common/sockopt"
)

func bindMarkToDialer(mark int, dialer *net.Dialer, _ string, destination netip.Addr) {
	addControlToDialer(dialer, bindMarkToControl(mark, destination))
}

func bindMarkToListenConfig(mark int, lc *net.ListenConfig, _, _ string) {
	addControlToListenConfig(lc, bindMarkToControl(mark, netip.Addr{}))
}

func bindMarkToControl(mark int, knownRemote netip.Addr) controlFn {
	return func(ctx context.Context, network, address string, c syscall.RawConn) (err error) {
		if knownRemote.IsValid() {
			if !knownRemote.Unmap().IsGlobalUnicast() {
				return
			}
		} else {
			addrPort, perr := netip.ParseAddrPort(address)
			if perr == nil && !addrPort.Addr().IsGlobalUnicast() {
				return
			}
		}

		return sockopt.RawConnMark(c, mark)
	}
}
