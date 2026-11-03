package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Miku0139oao/aster-core/component/dialer"
	"github.com/Miku0139oao/aster-core/constant/features"

	"github.com/metacubex/mipstack"
	wireguard "github.com/metacubex/sing-wireguard"
	"github.com/metacubex/wireguard-go/tun"
)

const (
	ipStackAuto   = "auto"
	ipStackGVisor = "gvisor"
	ipStackMips   = "mips"
)

// IPStackOption selects the userspace IP stack used by the L3 outbounds
// (wireguard, masque and openvpn).
type IPStackOption struct {
	Mode                 string `proxy:"mode,omitempty"`
	CongestionController string `proxy:"congestion-controller,omitempty"`
}

func (o *IPStackOption) normalize() {
	o.Mode = strings.ToLower(strings.TrimSpace(o.Mode))
	if o.Mode == "" {
		o.Mode = ipStackAuto
	}
	o.CongestionController = strings.ToLower(strings.TrimSpace(o.CongestionController))
}

func (o IPStackOption) validate() error {
	switch o.Mode {
	case ipStackAuto, ipStackMips:
	case ipStackGVisor:
		if !features.WithGVisor {
			return errors.New("gVisor IP stack requires the with_gvisor build tag")
		}
	default:
		return fmt.Errorf("invalid IP stack mode %q; expected auto, gvisor, or mips", o.Mode)
	}
	switch o.CongestionController {
	case "", mipstack.CongestionControlCUBIC, mipstack.CongestionControlReno, mipstack.CongestionControlBBR:
		return nil
	default:
		return fmt.Errorf("invalid IP stack congestion controller %q; expected cubic, reno, or bbr", o.CongestionController)
	}
}

// ipStack is the packet and socket surface shared by the gVisor stack device
// and the mihomo IP stack (MIPS). sing-wireguard's gVisor device already
// implements all of it; mipstack.Stack implements it directly with matching
// signatures.
type ipStack interface {
	Start() error
	DialTCP(ctx context.Context, network string, source, destination netip.AddrPort) (net.Conn, error)
	DialUDP(ctx context.Context, network string, source, destination netip.AddrPort) (net.Conn, error)
	ListenUDP(ctx context.Context, network string, local netip.AddrPort) (net.PacketConn, error)
	Read(buffers [][]byte, sizes []int, offset int) (int, error)
	Write(buffers [][]byte, offset int) (int, error)
	MTU() (int, error)
	Name() (string, error)
	BatchSize() int
	Close() error
}

// newIPStack constructs the selected userspace IP stack.
func newIPStack(option IPStackOption, localAddresses []netip.Prefix, mtu uint32) (ipStack, error) {
	mode := option.Mode
	if mode == ipStackAuto {
		// Aster keeps "auto" meaning gVisor when it is compiled in: gVisor is
		// the historical default stack for these outbounds and switching the
		// default to MIPS would silently change behaviour for every existing
		// config. "auto" only falls back to MIPS in builds without gVisor.
		if features.WithGVisor {
			mode = ipStackGVisor
		} else {
			mode = ipStackMips
		}
	}
	switch mode {
	case ipStackGVisor:
		return wireguard.NewStackDevice(localAddresses, mtu)
	case ipStackMips:
		return mipstack.New(mipstack.Config{
			LocalAddresses: localAddresses,
			MTU:            mtu,
			TCP: mipstack.TCPSocketDefaults{
				CongestionControl: option.CongestionController,
				// Align with sing-wireguard: enable keepalive with 15-second
				// idle/interval timing and gVisor's default probe count.
				KeepAlive: true,
				KeepAliveConfig: mipstack.KeepAliveConfig{
					Idle: 15 * time.Second, Interval: 15 * time.Second, Count: 9,
				},
			},
		})
	default:
		return nil, errors.New("invalid IP stack mode")
	}
}

var _ ipStack = (*mipstack.Stack)(nil)
var _ ipStack = (wireguard.Device)(nil)

// ipStackFactory builds the selected userspace IP stack. It is a variable so
// tests can assert when a stack is built; production code always ends up in
// newIPStack.
var ipStackFactory = newIPStack

// ipStackNetDialer adapts an ipStack to dialer.NetDialer.
type ipStackNetDialer struct {
	stack ipStack
}

var _ dialer.NetDialer = (*ipStackNetDialer)(nil)

func (d ipStackNetDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dst, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", address, err)
	}
	switch {
	case strings.HasPrefix(network, "tcp"):
		return d.stack.DialTCP(ctx, network, netip.AddrPort{}, dst)
	case strings.HasPrefix(network, "udp"):
		return d.stack.DialUDP(ctx, network, netip.AddrPort{}, dst)
	default:
		return nil, fmt.Errorf("invalid network %q", network)
	}
}

// wireguardDevice is the tun.Device + ipStack combination the wireguard-go
// device needs. sing-wireguard's gVisor device satisfies it directly; MIPS is
// wrapped in ipStackWireguardDevice.
type wireguardDevice interface {
	ipStack
	tun.Device
}

type ipStackWireguardDevice struct {
	ipStack
	events    chan tun.Event
	closeOnce sync.Once
}

func (d *ipStackWireguardDevice) File() *os.File {
	return nil
}

func (d *ipStackWireguardDevice) Events() <-chan tun.Event {
	return d.events
}

func (d *ipStackWireguardDevice) Start() error {
	d.events <- tun.EventUp
	return nil
}

func (d *ipStackWireguardDevice) Close() error {
	d.closeOnce.Do(func() {
		close(d.events)
	})
	return d.ipStack.Close()
}

func newWireguardDevice(stack ipStack) (wireguardDevice, error) {
	if wgDevice, ok := stack.(wireguardDevice); ok {
		return wgDevice, nil
	}
	// MIPS must be started here, before the wireguard-go device starts reading
	// from it; gVisor's Start is a no-op signal on an already running stack.
	if err := stack.Start(); err != nil {
		return nil, err
	}
	return &ipStackWireguardDevice{
		ipStack: stack,
		events:  make(chan tun.Event, 1),
	}, nil
}
