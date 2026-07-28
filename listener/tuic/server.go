package tuic

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/sockopt"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/sing"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/socks5"
	"github.com/Miku0139oao/aster-core/transport/tuic"

	"github.com/gofrs/uuid/v5"
	"github.com/metacubex/quic-go"
	"github.com/metacubex/tls"
	"golang.org/x/exp/slices"
)

const ServerMaxIncomingStreams = (1 << 32) - 1

type Listener struct {
	closed       atomic.Bool
	config       LC.TuicServer
	udpListeners []net.PacketConn
	servers      []*tuic.Server
	closeOnce    sync.Once
	closeErr     error
}

func New(config LC.TuicServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-TUIC"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.TUIC,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Time:       ntp.Now,
		MinVersion: tls.VersionTLS13,
	}
	certLoader, err := ca.NewTLSKeyPairLoader(config.Certificate, config.PrivateKey)
	if err != nil {
		return nil, err
	}
	tlsConfig.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certLoader()
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

	if config.EchKey != "" {
		err = ech.LoadECHKey(config.EchKey, tlsConfig)
		if err != nil {
			return nil, err
		}
	}
	if len(config.ALPN) > 0 {
		tlsConfig.NextProtos = config.ALPN
	} else {
		tlsConfig.NextProtos = []string{"h3"}
	}

	if config.MaxIdleTime == 0 {
		config.MaxIdleTime = 15000
	}
	if config.AuthenticationTimeout == 0 {
		config.AuthenticationTimeout = 1000
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:        time.Duration(config.MaxIdleTime) * time.Millisecond,
		MaxIncomingStreams:    ServerMaxIncomingStreams,
		MaxIncomingUniStreams: ServerMaxIncomingStreams,
		EnableDatagrams:       true,
		Allow0RTT:             true,
		DisablePathManager:    true, // for port hopping
	}
	quicConfig.InitialStreamReceiveWindow = tuic.DefaultStreamReceiveWindow / 10
	quicConfig.MaxStreamReceiveWindow = tuic.DefaultStreamReceiveWindow
	quicConfig.InitialConnectionReceiveWindow = tuic.DefaultConnectionReceiveWindow / 10
	quicConfig.MaxConnectionReceiveWindow = tuic.DefaultConnectionReceiveWindow

	packetOverHead := tuic.PacketOverHeadV4
	if len(config.Token) == 0 {
		packetOverHead = tuic.PacketOverHeadV5
	}

	if config.CWND == 0 {
		config.CWND = 32
	}

	if config.MaxUdpRelayPacketSize == 0 {
		config.MaxUdpRelayPacketSize = 1500
	}
	maxDatagramFrameSize := config.MaxUdpRelayPacketSize + packetOverHead
	if maxDatagramFrameSize > 1400 {
		maxDatagramFrameSize = 1400
	}
	config.MaxUdpRelayPacketSize = maxDatagramFrameSize - packetOverHead
	quicConfig.MaxDatagramFrameSize = int64(maxDatagramFrameSize)

	handleTcpFn := func(conn net.Conn, addr socks5.Addr, _additions ...inbound.Addition) error {
		//newAdditions := additions
		//if len(_additions) > 0 {
		//	newAdditions = slices.Clip(additions) // force the subsequent `append()` to copy the slice
		//	newAdditions = append(newAdditions, _additions...)
		//}
		//conn, metadata := inbound.NewSocket(addr, conn, C.TUIC, newAdditions...)
		//go tunnel.HandleTCPConn(conn, metadata)
		go h.HandleSocket(addr, conn, _additions...) // h.HandleSocket will block, so open a new goroutine
		return nil
	}
	handleUdpFn := func(addr socks5.Addr, packet C.UDPPacket, _additions ...inbound.Addition) error {
		newAdditions := additions
		if len(_additions) > 0 {
			newAdditions = slices.Clip(additions) // force the subsequent `append()` to copy the slice
			newAdditions = append(newAdditions, _additions...)
		}
		tunnel.HandleUDPPacket(inbound.NewPacket(addr, packet, C.TUIC, newAdditions...))
		return nil
	}

	option := &tuic.ServerOption{
		HandleTcpFn:           handleTcpFn,
		HandleUdpFn:           handleUdpFn,
		TlsConfig:             tlsConfig,
		QuicConfig:            quicConfig,
		CongestionController:  config.CongestionController,
		AuthenticationTimeout: time.Duration(config.AuthenticationTimeout) * time.Millisecond,
		MaxUdpRelayPacketSize: config.MaxUdpRelayPacketSize,
		CWND:                  config.CWND,
		BBRProfile:            config.BBRProfile,
	}
	if len(config.Token) > 0 {
		tokens := make([][32]byte, len(config.Token))
		for i, token := range config.Token {
			tokens[i] = tuic.GenTKN(token)
		}
		option.Tokens = tokens
	}
	if len(config.Users) > 0 {
		users := make(map[[16]byte]string)
		for _uuid, password := range config.Users {
			users[uuid.FromStringOrNil(_uuid)] = password
		}
		option.Users = users
	}

	sl = &Listener{config: config}
	created := sl
	defer func() {
		if err != nil {
			if closeErr := created.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		ul, err := lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return nil, err
		}

		if err := sockopt.UDPReuseaddr(ul); err != nil {
			log.Warnln("Failed to Reuse UDP Address: %s", err)
		}

		sl.udpListeners = append(sl.udpListeners, ul)

		var server *tuic.Server
		server, err = tuic.NewServer(option, ul)
		if err != nil {
			return nil, err
		}

		sl.servers = append(sl.servers, server)

		go func() {
			err := server.Serve()
			if err != nil {
				if created.closed.Load() {
					return
				}
			}
		}()
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		for _, lis := range l.servers {
			if err := lis.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		for _, lis := range l.udpListeners {
			if err := lis.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
	})
	return l.closeErr
}

func (l *Listener) Config() LC.TuicServer {
	return l.config
}

func (l *Listener) AddrList() (addrList []net.Addr) {
	for _, lis := range l.udpListeners {
		addrList = append(addrList, lis.LocalAddr())
	}
	return
}
