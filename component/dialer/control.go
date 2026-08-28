package dialer

import (
	"context"
	"net"
	"syscall"
)

type controlFn = func(ctx context.Context, network, address string, c syscall.RawConn) error

func addControlToListenConfig(lc *net.ListenConfig, fn controlFn) {
	if lc.Control == nil {
		lc.Control = func(network, address string, c syscall.RawConn) error {
			return fn(context.Background(), network, address, c)
		}
		return
	}
	prev := lc.Control
	lc.Control = func(network, address string, c syscall.RawConn) (err error) {
		if err = prev(network, address, c); err != nil {
			return
		}
		return fn(context.Background(), network, address, c)
	}
}

func addControlToDialer(d *net.Dialer, fn controlFn) {
	if d.ControlContext == nil && d.Control == nil {
		d.ControlContext = fn
		return
	}
	prevCtx := d.ControlContext
	prev := d.Control
	d.ControlContext = func(ctx context.Context, network, address string, c syscall.RawConn) (err error) {
		switch {
		case prevCtx != nil:
			if err = prevCtx(ctx, network, address, c); err != nil {
				return
			}
		case prev != nil:
			if err = prev(network, address, c); err != nil {
				return
			}
		}
		return fn(ctx, network, address, c)
	}
}
