package dialer

import (
	"context"
	"net"
	"net/netip"
	"syscall"

	"github.com/Miku0139oao/aster-core/component/iface"

	"golang.org/x/sys/unix"
)

func bindControl(ifaceIdx int, knownRemote netip.Addr) controlFn {
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
			switch network {
			case "tcp6", "udp6", "ip6":
				innerErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifaceIdx)
			default:
				innerErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifaceIdx)
			}
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

	ifaceObj, err := iface.ResolveInterface(ifaceName)
	if err != nil {
		return err
	}

	addControlToDialer(dialer, bindControl(ifaceObj.Index, destination))
	return nil
}

func bindIfaceToListenConfig(ifaceName string, lc *net.ListenConfig, _, address string, rAddrPort netip.AddrPort) (string, error) {
	ifaceObj, err := iface.ResolveInterface(ifaceName)
	if err != nil {
		return "", err
	}

	addControlToListenConfig(lc, bindControl(ifaceObj.Index, netip.Addr{}))
	return address, nil
}

func ParseNetwork(network string, addr netip.Addr) string {
	return network
}
