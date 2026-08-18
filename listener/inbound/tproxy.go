package inbound

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener/tproxy"
	"github.com/Miku0139oao/aster-core/log"
)

type TProxyOption struct {
	BaseOption
	UDP bool `inbound:"udp,omitempty"`
}

func (o TProxyOption) Equal(config C.InboundConfig) bool {
	return optionToString(o) == optionToString(config)
}

type TProxy struct {
	*Base
	config *TProxyOption
	lUDP   []*tproxy.UDPListener
	lTCP   []*tproxy.Listener
	udp    bool
	mu     sync.Mutex
}

func NewTProxy(options *TProxyOption) (*TProxy, error) {
	base, err := NewBase(&options.BaseOption)
	if err != nil {
		return nil, err
	}
	return &TProxy{
		Base:   base,
		config: options,
		udp:    options.UDP,
	}, nil
}

// Config implements constant.InboundListener
func (t *TProxy) Config() C.InboundConfig {
	return t.config
}

// Address implements constant.InboundListener
func (t *TProxy) Address() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.addressLocked()
}

func (t *TProxy) addressLocked() string {
	var addrList []string
	for _, l := range t.lTCP {
		addrList = append(addrList, l.Address())
	}
	return strings.Join(addrList, ",")
}

// Listen implements constant.InboundListener
func (t *TProxy) Listen(tunnel C.Tunnel) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tcpListeners := make([]*tproxy.Listener, 0, len(strings.Split(t.RawAddress(), ",")))
	udpListeners := make([]*tproxy.UDPListener, 0, len(strings.Split(t.RawAddress(), ",")))
	for _, addr := range strings.Split(t.RawAddress(), ",") {
		lTCP, err := tproxy.New(addr, tunnel, t.Additions()...)
		if err != nil {
			return errors.Join(err, closeTProxyListeners(tcpListeners, udpListeners))
		}
		tcpListeners = append(tcpListeners, lTCP)
		if t.udp {
			lUDP, err := tproxy.NewUDP(addr, tunnel, t.Additions()...)
			if err != nil {
				return errors.Join(err, closeTProxyListeners(tcpListeners, udpListeners))
			}
			udpListeners = append(udpListeners, lUDP)
		}
	}
	t.lTCP = tcpListeners
	t.lUDP = udpListeners
	log.Infoln("TProxy[%s] proxy listening at: %s", t.Name(), t.addressLocked())
	return nil
}

// Close implements constant.InboundListener
func (t *TProxy) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	tcpListeners, udpListeners := t.lTCP, t.lUDP
	t.lTCP, t.lUDP = nil, nil
	return closeTProxyListeners(tcpListeners, udpListeners)
}

func closeTProxyListeners(tcpListeners []*tproxy.Listener, udpListeners []*tproxy.UDPListener) error {
	var errs []error
	for _, l := range tcpListeners {
		err := l.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("close tcp listener %s err: %w", l.Address(), err))
		}
	}
	for _, l := range udpListeners {
		err := l.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("close udp listener %s err: %w", l.Address(), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

var _ C.InboundListener = (*TProxy)(nil)
