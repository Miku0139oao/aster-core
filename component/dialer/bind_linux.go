package dialer

import (
	"context"
	"net"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindControl(ifaceName string, knownRemote netip.Addr) controlFn {
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

		var innerErr error
		err = c.Control(func(fd uintptr) {
			innerErr = unix.BindToDevice(int(fd), ifaceName)
		})

		if innerErr != nil {
			err = innerErr
		}

		return
	}
}

func bindIfaceToDialer(ifaceName string, dialer *net.Dialer, _ string, destination netip.Addr) error {
	if destination.IsValid() && !destination.Unmap().IsGlobalUnicast() {
		return nil
	}

	addControlToDialer(dialer, bindControl(ifaceName, destination))

	return nil
}

func bindIfaceToListenConfig(ifaceName string, lc *net.ListenConfig, _, address string, rAddrPort netip.AddrPort) (string, error) {
	addControlToListenConfig(lc, bindControl(ifaceName, netip.Addr{}))

	return address, nil
}

func ParseNetwork(network string, addr netip.Addr) string {
	return network
}
