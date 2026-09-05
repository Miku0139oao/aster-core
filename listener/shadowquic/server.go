package shadowquic

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/sockopt"
	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/component/ca"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/inner"
	"github.com/Miku0139oao/aster-core/listener/sing"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/shadowquic"
	"github.com/Miku0139oao/aster-core/transport/socks5"
	"github.com/Miku0139oao/aster-core/transport/tuic"

	"github.com/metacubex/jls-quic-go"
	"github.com/metacubex/jls-tls"
)

const ServerMaxIncomingStreams = (1 << 32) - 1

type Listener struct {
	closed       atomic.Bool
	config       LC.ShadowQuicServer
	udpListeners []net.PacketConn
	servers      []*shadowquic.Server
	closeOnce    sync.Once
	closeErr     error
}

func New(config LC.ShadowQuicServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if strings.TrimSpace(config.JLSUpstream.Addr) == "" {
		return nil, errors.New("shadowquic: jls-upstream.addr is required")
	}
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-SHADOWQUIC"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.SHADOWQUIC,
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
	// JLS authenticates the peer, so this generated certificate only carries the TLS handshake.
	certificate, privateKey, _, err := ca.NewRandomTLSKeyPair(ca.KeyPairTypeP256)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair([]byte(certificate), []byte(privateKey))
	if err != nil {
		return nil, err
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	if len(config.ALPN) > 0 {
		tlsConfig.NextProtos = config.ALPN
	} else {
		tlsConfig.NextProtos = []string{"h3"}
	}
	users := make([]tls.JLSUser, 0, len(config.Users))
	for _, user := range config.Users {
		users = append(users, tls.JLSUser{
			Username: user.Username,
			Password: user.Password,
		})
	}
	serverName := config.JLSUpstream.SNI
	if serverName == "" {
		serverName = defaultJLSServerName(config.JLSUpstream.Addr)
	}
	tlsConfig.JLSConfig = &tls.JLSConfig{
		Enable:     true,
		Users:      users,
		ServerName: serverName,
	}

	if config.MaxIdleTime == 0 {
		config.MaxIdleTime = 30000
	}
	if config.MaxDatagramFrameSize == 0 {
		config.MaxDatagramFrameSize = 1400
	}
	if config.CWND == 0 {
		config.CWND = 32
	}
	jlsPacketDialer := func(ctx context.Context, network, address string) (net.PacketConn, net.Addr, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return inner.HandleUdp(tunnel, network, address, config.JLSUpstream.Proxy)
	}
	quicVersions, _, getVersionNegotiationProfile, err := shadowquic.ResolveQUICVersionProfile(
		config.QUICVersions,
		config.JLSUpstream.Addr,
		config.JLSUpstream.QUICVersionProbe,
		jlsPacketDialer,
		log.Warnln,
	)
	if err != nil {
		return nil, err
	}
	// Newer jls-quic-go forwards unsupported versions to the camouflage
	// upstream itself, so the local VN profile hook is gone. Apply a probed
	// profile to Config.Versions so authenticated JLS clients are still
	// accepted locally.
	if getVersionNegotiationProfile != nil {
		if probed, _ := getVersionNegotiationProfile(); len(probed) > 0 {
			quicVersions = probed
		}
	}

	quicConfig := &quic.Config{
		Versions: quicVersions,
		JLSConfig: &quic.JLSConfig{
			UpstreamAddr: config.JLSUpstream.Addr,
			RateLimit:    config.JLSUpstream.RateLimit,
			PacketDialer: jlsPacketDialer,
		},
		MaxIdleTimeout:                 time.Duration(config.MaxIdleTime) * time.Millisecond,
		MaxIncomingStreams:             ServerMaxIncomingStreams,
		MaxIncomingUniStreams:          ServerMaxIncomingStreams,
		InitialStreamReceiveWindow:     uint64(config.ReceiveWindowConn),
		MaxStreamReceiveWindow:         uint64(config.ReceiveWindowConn),
		InitialConnectionReceiveWindow: uint64(config.ReceiveWindow),
		MaxConnectionReceiveWindow:     uint64(config.ReceiveWindow),
		MaxDatagramFrameSize:           int64(config.MaxDatagramFrameSize),
		EnableDatagrams:                true,
		Allow0RTT:                      config.ZeroRTT,
		DisablePathMTUDiscovery:        config.DisableMTUDiscovery,
	}
	if config.ReceiveWindowConn == 0 {
		quicConfig.InitialStreamReceiveWindow = tuic.DefaultStreamReceiveWindow / 10
		quicConfig.MaxStreamReceiveWindow = tuic.DefaultStreamReceiveWindow
	}
	if config.ReceiveWindow == 0 {
		quicConfig.InitialConnectionReceiveWindow = tuic.DefaultConnectionReceiveWindow / 10
		quicConfig.MaxConnectionReceiveWindow = tuic.DefaultConnectionReceiveWindow
	}
	handleTcpFn := func(conn net.Conn, addr socks5.Addr, _additions ...inbound.Addition) error {
		go h.HandleSocket(addr, conn, _additions...)
		return nil
	}
	handleUdpFn := func(addr socks5.Addr, packet C.UDPPacket, _additions ...inbound.Addition) error {
		newAdditions := additions
		if len(_additions) > 0 {
			newAdditions = append(append([]inbound.Addition{}, additions...), _additions...)
		}
		tunnel.HandleUDPPacket(inbound.NewPacket(addr, packet, C.SHADOWQUIC, newAdditions...))
		return nil
	}

	option := &shadowquic.ServerOption{
		HandleTcpFn:           handleTcpFn,
		HandleUdpFn:           handleUdpFn,
		TLSConfig:             tlsConfig,
		QUICConfig:            quicConfig,
		CongestionController:  config.CongestionController,
		SendBPS:               utils.StringToBps(config.Up),
		ReceiveBPS:            utils.StringToBps(config.Down),
		IgnoreClientBandwidth: config.IgnoreClientBandwidth,
		CWND:                  config.CWND,
		BBRProfile:            config.BBRProfile,
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
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		ul, err := lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return nil, err
		}
		if err := sockopt.UDPReuseaddr(ul); err != nil {
			log.Warnln("Failed to Reuse UDP Address: %s", err)
		}
		sl.udpListeners = append(sl.udpListeners, ul)

		server, err := shadowquic.NewServer(option, ul)
		if err != nil {
			return nil, err
		}
		sl.servers = append(sl.servers, server)

		go func() {
			err := server.Serve() // Serve only returns on an accept error.
			if !created.closed.Load() {
				log.Warnln("ShadowQuic server closed: %s", err)
			}
		}()
	}
	return sl, nil
}

func defaultJLSServerName(upstreamAddr string) string {
	host, _, err := net.SplitHostPort(upstreamAddr)
	if err != nil {
		host = upstreamAddr
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	if strings.Contains(host, ":") {
		return ""
	}
	return host
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		for _, server := range l.servers {
			if err := server.Close(); err != nil {
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

func (l *Listener) Config() LC.ShadowQuicServer {
	return l.config
}

func (l *Listener) AddrList() (addrList []net.Addr) {
	for _, lis := range l.udpListeners {
		addrList = append(addrList, lis.LocalAddr())
	}
	return
}
