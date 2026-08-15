package sing_vless

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/jls"
	"github.com/Miku0139oao/aster-core/listener/reality"
	"github.com/Miku0139oao/aster-core/listener/restls"
	"github.com/Miku0139oao/aster-core/listener/shadowtls"
	"github.com/Miku0139oao/aster-core/listener/sing"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/gun"
	"github.com/Miku0139oao/aster-core/transport/vless/encryption"
	mihomoVMess "github.com/Miku0139oao/aster-core/transport/vmess"
	"github.com/Miku0139oao/aster-core/transport/xhttp"

	"github.com/metacubex/http"
	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
	"golang.org/x/exp/slices"
)

type Listener struct {
	closed     atomic.Bool
	closeOnce  sync.Once
	closeErr   error
	config     LC.VlessServer
	listeners  []net.Listener
	httpServer *http.Server
	service    *Service[string]
	decryption *encryption.ServerInstance
	transports []*N.ConnectionTrackingListener
}

type closeOnceListener struct {
	net.Listener
	once sync.Once
	err  error
}

func (l *closeOnceListener) Close() error {
	l.once.Do(func() {
		l.err = l.Listener.Close()
	})
	return l.err
}

func New(config LC.VlessServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-VLESS"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.VLESS,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}

	service := NewService[string](h)
	err = service.UpdateUsers(
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.Username
		}),
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.UUID
		}),
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.Flow
		}))
	if err != nil {
		return nil, err
	}

	sl = &Listener{config: config, service: service}
	created := sl
	defer func() {
		if err != nil {
			_ = created.Close()
		}
	}()

	sl.decryption, err = encryption.NewServer(config.Decryption)
	if err != nil {
		return nil, err
	}

	httpServer := http.Server{
		IdleTimeout: 30 * time.Second,
		Protocols:   new(http.Protocols),
	}
	sl.httpServer = &httpServer
	tlsConfig := &tls.Config{Time: ntp.Now}
	var shadowTLSBuilder *shadowtls.Builder
	var restlsBuilder *restls.Builder
	var jlsBuilder *jls.Builder
	var realityBuilder *reality.Builder

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
	if tlsConfig.ClientAuth != tls.NoClientCert && tlsConfig.GetCertificate == nil {
		return nil, errors.New("client-auth requires certificate")
	}
	securityModes := make([]string, 0, 5)
	if tlsConfig.GetCertificate != nil {
		securityModes = append(securityModes, "certificate")
	}
	if config.RealityConfig.PrivateKey != "" {
		securityModes = append(securityModes, "reality")
	}
	if config.ShadowTLS.Enable {
		securityModes = append(securityModes, "shadow-tls")
	}
	if config.ResTLS.Enable {
		securityModes = append(securityModes, "res-tls")
	}
	if config.JLSConfig.Enable {
		securityModes = append(securityModes, "jls")
	}
	if len(securityModes) > 1 {
		return nil, errors.New("security modes are mutually exclusive: " + strings.Join(securityModes, ", "))
	}
	if len(securityModes) == 0 && sl.decryption == nil && !config.AllowInsecure {
		return nil, errors.New("disallow using Vless without any certificates/shadow-tls/res-tls/jls/reality/decryption/allow-insecure config")
	}
	if config.RealityConfig.PrivateKey != "" {
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.ShadowTLS.Enable {
		shadowTLSBuilder, err = shadowtls.New(config.ShadowTLS, tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.ResTLS.Enable {
		restlsBuilder = restls.New(config.ResTLS, tunnel)
	}
	if config.JLSConfig.Enable {
		jlsBuilder, err = jls.New(config.JLSConfig, tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.WsPath != "" {
		httpMux := http.NewServeMux()
		httpMux.HandleFunc(config.WsPath, func(w http.ResponseWriter, r *http.Request) {
			conn, err := mihomoVMess.StreamUpgradedWebsocketConn(w, r)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			sl.HandleConn(conn, tunnel, additions...)
		})
		httpServer.Handler = httpMux
		httpServer.Protocols.SetHTTP1(true)
		tlsConfig.NextProtos = append(tlsConfig.NextProtos, "http/1.1")
	}
	if config.GrpcServiceName != "" {
		httpServer.Handler = gun.NewServerHandler(gun.ServerOption{
			ServiceName: config.GrpcServiceName,
			ConnHandler: func(conn net.Conn) {
				sl.HandleConn(conn, tunnel, additions...)
			},
			HttpHandler: httpServer.Handler,
		})
		httpServer.Protocols.SetHTTP2(true)
		// SetUnencryptedHTTP2 to ensure we can work in plain http2 and some tls conn is not *tls.Conn (like *reality.Conn)
		//
		// Enable HTTP/2 support unconditionally on the server.
		//
		// Note that this usage is limited to our own net/http fork
		// The standard library also needs to mask the tls.Conn type for the conn returned by the Listener.
		// see: https://github.com/golang/go/issues/79293#issuecomment-4426393534
		httpServer.Protocols.SetUnencryptedHTTP2(true)
		tlsConfig.NextProtos = append([]string{"h2"}, tlsConfig.NextProtos...) // h2 must before http/1.1
	}
	if config.XHTTPConfig.Mode != "" {
		switch config.XHTTPConfig.Mode {
		case "auto", "stream-up", "stream-one", "packet-up":
		default:
			return nil, errors.New("unsupported xhttp mode")
		}
	}
	if config.XHTTPConfig.Path != "" || config.XHTTPConfig.Host != "" || config.XHTTPConfig.Mode != "" {
		httpServer.Handler, err = xhttp.NewServerHandler(xhttp.ServerOption{
			Config: xhttp.Config{
				Host:                 config.XHTTPConfig.Host,
				Path:                 config.XHTTPConfig.Path,
				Mode:                 config.XHTTPConfig.Mode,
				XPaddingBytes:        config.XHTTPConfig.XPaddingBytes,
				XPaddingObfsMode:     config.XHTTPConfig.XPaddingObfsMode,
				XPaddingKey:          config.XHTTPConfig.XPaddingKey,
				XPaddingHeader:       config.XHTTPConfig.XPaddingHeader,
				XPaddingPlacement:    config.XHTTPConfig.XPaddingPlacement,
				XPaddingMethod:       config.XHTTPConfig.XPaddingMethod,
				UplinkHTTPMethod:     config.XHTTPConfig.UplinkHTTPMethod,
				SessionPlacement:     config.XHTTPConfig.SessionPlacement,
				SessionKey:           config.XHTTPConfig.SessionKey,
				SeqPlacement:         config.XHTTPConfig.SeqPlacement,
				SeqKey:               config.XHTTPConfig.SeqKey,
				UplinkDataPlacement:  config.XHTTPConfig.UplinkDataPlacement,
				UplinkDataKey:        config.XHTTPConfig.UplinkDataKey,
				UplinkChunkSize:      config.XHTTPConfig.UplinkChunkSize,
				NoSSEHeader:          config.XHTTPConfig.NoSSEHeader,
				ScStreamUpServerSecs: config.XHTTPConfig.ScStreamUpServerSecs,
				ScMaxBufferedPosts:   config.XHTTPConfig.ScMaxBufferedPosts,
				ScMaxEachPostBytes:   config.XHTTPConfig.ScMaxEachPostBytes,
			},
			ConnHandler: func(conn net.Conn) {
				sl.HandleConn(conn, tunnel, additions...)
			},
			HttpHandler: httpServer.Handler,
		})
		if err != nil {
			return nil, err
		}
		httpServer.Protocols.SetHTTP1(true)
		httpServer.Protocols.SetHTTP2(true)
		// SetUnencryptedHTTP2 to ensure we can work in plain http2 and some tls conn is not *tls.Conn (like *reality.Conn)
		//
		// Enable HTTP/2 support unconditionally on the server.
		//
		// Note that this usage is limited to our own net/http fork
		// The standard library also needs to mask the tls.Conn type for the conn returned by the Listener.
		// see: https://github.com/golang/go/issues/79293#issuecomment-4426393534
		httpServer.Protocols.SetUnencryptedHTTP2(true)
		if !slices.Contains(tlsConfig.NextProtos, "http/1.1") {
			tlsConfig.NextProtos = append([]string{"http/1.1"}, tlsConfig.NextProtos...)
		}
		if !slices.Contains(tlsConfig.NextProtos, "h2") {
			tlsConfig.NextProtos = append([]string{"h2"}, tlsConfig.NextProtos...)
		}
	}
	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		// TCP
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		if shadowTLSBuilder != nil || restlsBuilder != nil || jlsBuilder != nil || realityBuilder != nil {
			transport := N.NewConnectionTrackingListener(l)
			sl.transports = append(sl.transports, transport)
			l = transport
		}
		if shadowTLSBuilder != nil {
			l = shadowTLSBuilder.NewListener(l)
		} else if restlsBuilder != nil {
			l = restlsBuilder.NewListener(l)
		} else if jlsBuilder != nil {
			l = jlsBuilder.NewListener(l)
		} else if realityBuilder != nil {
			l = realityBuilder.NewListener(l)
		} else if tlsConfig.GetCertificate != nil {
			l = tls.NewListener(l, tlsConfig)
		}
		if httpServer.Handler != nil {
			l = &closeOnceListener{Listener: l}
		}
		sl.listeners = append(sl.listeners, l)

		go func() {
			if httpServer.Handler != nil {
				_ = httpServer.Serve(l)
				return
			}
			for {
				c, err := l.Accept()
				if err != nil {
					if sl.closed.Load() {
						break
					}
					continue
				}

				go sl.HandleConn(c, tunnel)
			}
		}()
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		l.service.Close()
		l.closeTransportConnections()
		for _, lis := range l.listeners {
			if err := lis.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		if l.httpServer != nil {
			if err := l.httpServer.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		if l.decryption != nil {
			_ = l.decryption.Close()
		}
	})
	return l.closeErr
}

func (l *Listener) UpdateUsers(users []LC.VlessUser) error {
	err := l.service.UpdateUsers(
		common.Map(users, func(it LC.VlessUser) string {
			return it.Username
		}),
		common.Map(users, func(it LC.VlessUser) string {
			return it.UUID
		}),
		common.Map(users, func(it LC.VlessUser) string {
			return it.Flow
		}),
	)
	if err != nil {
		return err
	}
	l.closeTransportConnections()
	return nil
}

func (l *Listener) closeTransportConnections() {
	for _, transport := range l.transports {
		transport.CloseConnections()
	}
}

func (l *Listener) Config() string {
	return l.config.String()
}

func (l *Listener) AddrList() (addrList []net.Addr) {
	for _, lis := range l.listeners {
		addrList = append(addrList, lis.Addr())
	}
	return
}

func (l *Listener) HandleConn(conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) {
	rawConn := conn
	defer rawConn.Close()
	pending, err := l.service.trackPendingConnection(rawConn)
	if err != nil {
		return
	}
	defer l.service.untrackPendingConnection(pending)

	ctx := sing.WithAdditions(context.TODO(), additions...)
	if l.decryption != nil {
		conn, err = l.decryption.Handshake(conn, nil)
		if err != nil {
			return
		}
	}
	err = l.service.newConnection(ctx, conn, metadata.Metadata{
		Protocol: "vless",
		Source:   metadata.SocksaddrFromNet(conn.RemoteAddr()),
	}, pending)
	if err != nil {
		return
	}
}
