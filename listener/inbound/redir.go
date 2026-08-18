package inbound

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener/redir"
	"github.com/Miku0139oao/aster-core/log"
)

type RedirOption struct {
	BaseOption
}

func (o RedirOption) Equal(config C.InboundConfig) bool {
	return optionToString(o) == optionToString(config)
}

type Redir struct {
	*Base
	config *RedirOption
	l      []*redir.Listener
	mu     sync.Mutex
}

func NewRedir(options *RedirOption) (*Redir, error) {
	base, err := NewBase(&options.BaseOption)
	if err != nil {
		return nil, err
	}
	return &Redir{
		Base:   base,
		config: options,
	}, nil
}

// Config implements constant.InboundListener
func (r *Redir) Config() C.InboundConfig {
	return r.config
}

// Address implements constant.InboundListener
func (r *Redir) Address() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addressLocked()
}

func (r *Redir) addressLocked() string {
	var addrList []string
	for _, l := range r.l {
		addrList = append(addrList, l.Address())
	}
	return strings.Join(addrList, ",")
}

// Listen implements constant.InboundListener
func (r *Redir) Listen(tunnel C.Tunnel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	listeners := make([]*redir.Listener, 0, len(strings.Split(r.RawAddress(), ",")))
	for _, addr := range strings.Split(r.RawAddress(), ",") {
		l, err := redir.New(addr, tunnel, r.Additions()...)
		if err != nil {
			return errors.Join(err, closeRedirListeners(listeners))
		}
		listeners = append(listeners, l)
	}
	r.l = listeners
	log.Infoln("Redir[%s] proxy listening at: %s", r.Name(), r.addressLocked())
	return nil
}

// Close implements constant.InboundListener
func (r *Redir) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	listeners := r.l
	r.l = nil
	return closeRedirListeners(listeners)
}

func closeRedirListeners(listeners []*redir.Listener) error {
	var errs []error
	for _, l := range listeners {
		err := l.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("close redir listener %s err: %w", l.Address(), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

var _ C.InboundListener = (*Redir)(nil)
