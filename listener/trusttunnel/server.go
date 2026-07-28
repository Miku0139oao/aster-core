package trusttunnel

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/sockopt"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/sing"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/trusttunnel"

	"github.com/metacubex/tls"
)

type Listener struct {
	closed       bool
	config       LC.TrustTunnelServer
	listeners    []net.Listener
	udpListeners []net.PacketConn
	tlsConfig    *tls.Config
	services     []*trusttunnel.Service
}

func New(config LC.TrustTunnelServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-TRUSTTUNNEL"),
			inbound.WithSpecialRules(""),
		}
	}

	tlsConfig := &tls.Config{Time: ntp.Now}
	if config.Certificate != "" && config.PrivateKey != "" {
		certLoader, err := ca.NewTLSKeyPairLoader(config.Certificate, config.PrivateKey)
		if err != nil {
			return nil, err
		}
		tlsConfig.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return certLoader()
		}

		if config.EchKey != "" {
			err = ech.LoadECHKey(config.EchKey, tlsConfig)
			if err != nil {
				return nil, err
			}
		}
	}
	tlsConfig.ClientAuth = ca.ClientAuthTypeFromString(config.ClientAuthType)
	if len(config.ClientAuthCert) > 0 {
		if tlsConfig.ClientAuth == tls.NoClientCert {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	if tlsConfig.ClientAuth == tls.VerifyClientCertIfGiven || tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		pool, err := ca.LoadCertificates(config.ClientAuthCert)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = pool
	}

	sl = &Listener{
		config:    config,
		tlsConfig: tlsConfig,
	}

	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.TRUSTTUNNEL,
		Additions: additions,
	})
	if err != nil {
		return nil, err
	}

	if tlsConfig.GetCertificate == nil {
		return nil, errors.New("disallow using TrustTunnel without certificates config")
	}

	if len(config.Network) == 0 {
		config.Network = []string{"tcp"}
	}
	listenTCP, listenUDP := false, false
	for _, network := range config.Network {
		network = strings.ToLower(network)
		switch {
		case strings.HasPrefix(network, "tcp"):
			listenTCP = true
		case strings.HasPrefix(network, "udp"):
			listenUDP = true
		}
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		var (
			tcpListener net.Listener
			udpConn     net.PacketConn
		)
		if listenTCP {
			tcpListener, err = lc.Listen(context.Background(), "tcp", addr)
			if err != nil {
				return nil, errors.Join(err, sl.Close())
			}
		}
		if listenUDP {
			udpConn, err = lc.ListenPacket(context.Background(), "udp", addr)
			if err != nil {
				var tcpCloseErr error
				if tcpListener != nil {
					tcpCloseErr = tcpListener.Close()
				}
				return nil, errors.Join(err, tcpCloseErr, sl.Close())
			}

			if err := sockopt.UDPReuseaddr(udpConn); err != nil {
				log.Warnln("Failed to Reuse UDP Address: %s", err)
			}
		}

		service := trusttunnel.NewService(trusttunnel.ServiceOptions{
			Ctx:                   context.Background(),
			Logger:                log.SingLogger,
			Handler:               h,
			ICMPHandler:           nil,
			QUICCongestionControl: config.CongestionController,
			QUICCwnd:              config.CWND,
			QUICBBRProfile:        config.BBRProfile,
		})
		service.UpdateUsers(config.Users)
		sl.services = append(sl.services, service)
		err = service.Start(tcpListener, udpConn, tlsConfig)
		if err != nil {
			return nil, errors.Join(err, sl.Close())
		}

		if tcpListener != nil {
			sl.listeners = append(sl.listeners, tcpListener)
		}
		if udpConn != nil {
			sl.udpListeners = append(sl.udpListeners, udpConn)
		}
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closed = true
	services := l.services
	l.services = nil
	l.listeners = nil
	l.udpListeners = nil
	var errs []error
	for _, service := range services {
		if err := service.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *Listener) Config() string {
	return l.config.String()
}

func (l *Listener) AddrList() (addrList []net.Addr) {
	for _, lis := range l.listeners {
		addrList = append(addrList, lis.Addr())
	}
	for _, lis := range l.udpListeners {
		addrList = append(addrList, lis.LocalAddr())
	}
	return
}
