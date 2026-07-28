package inbound

import (
	"errors"
	"fmt"
	"strings"

	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/socks"
	"github.com/Miku0139oao/aster-core/log"
)

type SocksOption struct {
	BaseOption
	Users          AuthUsers     `inbound:"users,omitempty"`
	UDP            bool          `inbound:"udp,omitempty"`
	Certificate    string        `inbound:"certificate,omitempty"`
	PrivateKey     string        `inbound:"private-key,omitempty"`
	ClientAuthType string        `inbound:"client-auth-type,omitempty"`
	ClientAuthCert string        `inbound:"client-auth-cert,omitempty"`
	EchKey         string        `inbound:"ech-key,omitempty"`
	RealityConfig  RealityConfig `inbound:"reality-config,omitempty"`
}

func (o SocksOption) Equal(config C.InboundConfig) bool {
	return optionToString(o) == optionToString(config)
}

type Socks struct {
	*Base
	config *SocksOption
	udp    bool
	stl    []*socks.Listener
	sul    []*socks.UDPListener
}

func NewSocks(options *SocksOption) (*Socks, error) {
	base, err := NewBase(&options.BaseOption)
	if err != nil {
		return nil, err
	}
	return &Socks{
		Base:   base,
		config: options,
		udp:    options.UDP,
	}, nil
}

// Config implements constant.InboundListener
func (s *Socks) Config() C.InboundConfig {
	return s.config
}

// Close implements constant.InboundListener
func (s *Socks) Close() error {
	tcpListeners, udpListeners := s.stl, s.sul
	s.stl, s.sul = nil, nil
	return closeSocksListeners(tcpListeners, udpListeners)
}

func closeSocksListeners(tcpListeners []*socks.Listener, udpListeners []*socks.UDPListener) error {
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

// Address implements constant.InboundListener
func (s *Socks) Address() string {
	var addrList []string
	for _, l := range s.stl {
		addrList = append(addrList, l.Address())
	}
	return strings.Join(addrList, ",")
}

// Listen implements constant.InboundListener
func (s *Socks) Listen(tunnel C.Tunnel) error {
	lc := s.ListenConfig()
	tcpListeners := make([]*socks.Listener, 0, len(strings.Split(s.RawAddress(), ",")))
	udpListeners := make([]*socks.UDPListener, 0, len(strings.Split(s.RawAddress(), ",")))
	for _, addr := range strings.Split(s.RawAddress(), ",") {
		config := LC.AuthServer{
			Enable:         true,
			Listen:         addr,
			AuthStore:      s.config.Users.GetAuthStore(),
			Certificate:    s.config.Certificate,
			PrivateKey:     s.config.PrivateKey,
			ClientAuthType: s.config.ClientAuthType,
			ClientAuthCert: s.config.ClientAuthCert,
			EchKey:         s.config.EchKey,
			RealityConfig:  s.config.RealityConfig.Build(),
		}
		stl, err := socks.NewWithConfig(config, lc, tunnel, s.Additions()...)
		if err != nil {
			return errors.Join(err, closeSocksListeners(tcpListeners, udpListeners))
		}
		tcpListeners = append(tcpListeners, stl)
		if s.udp {
			sul, err := socks.NewUDPWithConfig(config, lc, tunnel, s.Additions()...)
			if err != nil {
				return errors.Join(err, closeSocksListeners(tcpListeners, udpListeners))
			}
			udpListeners = append(udpListeners, sul)
		}
	}
	s.stl = tcpListeners
	s.sul = udpListeners

	log.Infoln("SOCKS[%s] proxy listening at: %s", s.Name(), s.Address())
	return nil
}

var _ C.InboundListener = (*Socks)(nil)
