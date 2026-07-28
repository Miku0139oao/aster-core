package sing_vmess

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/jls"
	"github.com/Miku0139oao/aster-core/listener/reality"
	"github.com/Miku0139oao/aster-core/listener/restls"
	"github.com/Miku0139oao/aster-core/listener/shadowtls"
	"github.com/Miku0139oao/aster-core/listener/sing"
	"github.com/Miku0139oao/aster-core/listener/tlsmirror"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/gun"
	"github.com/Miku0139oao/aster-core/transport/mekya"
	"github.com/Miku0139oao/aster-core/transport/mkcp"
	mihomoVMess "github.com/Miku0139oao/aster-core/transport/vmess"

	"github.com/metacubex/http"
	"github.com/metacubex/mhurl"
	vmess "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
	"golang.org/x/exp/slices"
)

type Listener struct {
	closed     atomic.Bool
	config     LC.VmessServer
	listeners  []net.Listener
	httpServer *http.Server
	service    *vmess.Service[string]
	closeOnce  sync.Once
	closeErr   error
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

var _listener *Listener

func New(config LC.VmessServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-VMESS"),
			inbound.WithSpecialRules(""),
		}
		defer func() {
			_listener = sl
		}()
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.VMESS,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}
	if config.MekyaConfig.Enable {
		if config.MKCPConfig.Enable {
			return nil, errors.New("mkcp-config is unavailable in mekya")
		}
		if config.WsPath != "" || config.GrpcServiceName != "" {
			return nil, errors.New("ws and grpc are unavailable in mekya")
		}
	}

	service := vmess.NewService[string](h, vmess.ServiceWithDisableHeaderProtection(), vmess.ServiceWithTimeFunc(ntp.Now))
	err = service.UpdateUsers(
		common.Map(config.Users, func(it LC.VmessUser) string {
			return it.Username
		}),
		common.Map(config.Users, func(it LC.VmessUser) string {
			return it.UUID
		}),
		common.Map(config.Users, func(it LC.VmessUser) int {
			return it.AlterID
		}))
	if err != nil {
		return nil, err
	}

	sl = &Listener{config: config, service: service}
	created := sl
	defer func() {
		if err != nil {
			if closeErr := created.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	err = service.Start()
	if err != nil {
		return nil, err
	}

	httpServer := http.Server{
		IdleTimeout: 30 * time.Second,
		Protocols:   new(http.Protocols),
	}
	tlsConfig := &tls.Config{Time: ntp.Now}
	var shadowTLSBuilder *shadowtls.Builder
	var restlsBuilder *restls.Builder
	var jlsBuilder *jls.Builder
	var realityBuilder *reality.Builder
	var tlsMirrorBuilder *tlsmirror.Builder

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
	securityModes := make([]string, 0, 6)
	if tlsConfig.GetCertificate != nil {
		securityModes = append(securityModes, "certificate")
	}
	if config.RealityConfig.PrivateKey != "" {
		securityModes = append(securityModes, "reality")
	}
	if config.TLSMirrorConfig.PrimaryKey != "" {
		securityModes = append(securityModes, "tlsmirror")
	}
	tcpOnlySecurityMode := ""
	if config.ShadowTLS.Enable {
		securityModes = append(securityModes, "shadow-tls")
		tcpOnlySecurityMode = "ShadowTLS"
	}
	if config.ResTLS.Enable {
		securityModes = append(securityModes, "res-tls")
		tcpOnlySecurityMode = "Restls"
	}
	if config.JLSConfig.Enable {
		securityModes = append(securityModes, "jls")
		tcpOnlySecurityMode = "JLS"
	}
	if len(securityModes) > 1 {
		return nil, errors.New("security modes are mutually exclusive: " + strings.Join(securityModes, ", "))
	}
	if config.MKCPConfig.Enable && tcpOnlySecurityMode != "" {
		return nil, errors.New(tcpOnlySecurityMode + " only supports TCP transports")
	}
	if config.RealityConfig.PrivateKey != "" {
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.TLSMirrorConfig.PrimaryKey != "" {
		tlsMirrorBuilder = tlsmirror.Config{
			PrimaryKey:                    config.TLSMirrorConfig.PrimaryKey,
			Dest:                          config.TLSMirrorConfig.Dest,
			Proxy:                         config.TLSMirrorConfig.Proxy,
			ExplicitNonceCipherSuites:     config.TLSMirrorConfig.ExplicitNonceCipherSuites,
			DeferInstanceDerivedWriteTime: config.TLSMirrorConfig.DeferInstanceDerivedWriteTime.Build(),
			TransportLayerPadding:         config.TLSMirrorConfig.TransportLayerPadding.Build(),
			ConnectionEnrolment:           config.TLSMirrorConfig.ConnectionEnrolment.Build(),
			SequenceWatermarkingEnabled:   config.TLSMirrorConfig.SequenceWatermarkingEnabled,
		}.Build(tunnel)
		h.Tunnel = tlsMirrorBuilder.WrapTunnel(tunnel)
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
			created.HandleConn(conn, tunnel, additions...)
		})
		httpServer.Handler = httpMux
		httpServer.Protocols.SetHTTP1(true)
		tlsConfig.NextProtos = append(tlsConfig.NextProtos, "http/1.1")
	}
	if config.GrpcServiceName != "" {
		httpServer.Handler = gun.NewServerHandler(gun.ServerOption{
			ServiceName: config.GrpcServiceName,
			ConnHandler: func(conn net.Conn) {
				created.HandleConn(conn, tunnel, additions...)
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
	if config.MekyaConfig.Enable {
		if !slices.Contains(tlsConfig.NextProtos, "http/1.1") {
			tlsConfig.NextProtos = append([]string{"http/1.1"}, tlsConfig.NextProtos...)
		}
		if !slices.Contains(tlsConfig.NextProtos, "h2") {
			tlsConfig.NextProtos = append([]string{"h2"}, tlsConfig.NextProtos...)
		}
	}
	if httpServer.Handler != nil {
		sl.httpServer = &httpServer
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		// TCP
		var l net.Listener
		if config.MKCPConfig.Enable {
			pc, err := lc.ListenPacket(context.Background(), "udp", addr)
			if err != nil {
				return nil, err
			}
			l, err = mkcp.Listen(context.Background(), pc, config.MKCPConfig.Build())
			if err != nil {
				if closeErr := pc.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
				return nil, err
			}
		} else {
			l, err = lc.Listen(context.Background(), "tcp", addr)
			if err != nil {
				return nil, err
			}
		}
		sl.listeners = append(sl.listeners, l)
		listenerIndex := len(sl.listeners) - 1
		if shadowTLSBuilder != nil {
			l = shadowTLSBuilder.NewListener(l)
		} else if restlsBuilder != nil {
			l = restlsBuilder.NewListener(l)
		} else if jlsBuilder != nil {
			l = jlsBuilder.NewListener(l)
		} else if tlsMirrorBuilder != nil {
			l = tlsMirrorBuilder.NewListener(l)
		} else if realityBuilder != nil {
			l = realityBuilder.NewListener(l)
		} else if tlsConfig.GetCertificate != nil {
			l = tls.NewListener(l, tlsConfig)
		}
		if config.MekyaConfig.Enable {
			l, err = mekya.Listen(context.Background(), l, config.MekyaConfig.Build())
			if err != nil {
				return nil, err
			}
		}
		if httpServer.Handler != nil {
			l = &closeOnceListener{Listener: l}
		}
		sl.listeners[listenerIndex] = l

		go func() {
			if httpServer.Handler != nil {
				_ = httpServer.Serve(l)
				return
			}
			for {
				c, err := l.Accept()
				if err != nil {
					if created.closed.Load() {
						break
					}
					continue
				}

				go created.HandleConn(c, tunnel)
			}
		}()
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		if l.httpServer != nil {
			if err := l.httpServer.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		for _, lis := range l.listeners {
			if err := lis.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		if l.service != nil {
			if err := l.service.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
	})
	return l.closeErr
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
	ctx := sing.WithAdditions(context.TODO(), additions...)
	err := l.service.NewConnection(ctx, conn, metadata.Metadata{
		Protocol: "vmess",
		Source:   metadata.SocksaddrFromNet(conn.RemoteAddr()),
	})
	if err != nil {
		_ = conn.Close()
		return
	}
}

func HandleVmess(conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) bool {
	if _listener != nil && _listener.service != nil {
		go _listener.HandleConn(conn, tunnel, additions...)
		return true
	}
	return false
}

func ParseVmessURL(s string) (addr, username, password string, err error) {
	u, err := mhurl.Parse(s) // we need multiple hosts url supports
	if err != nil {
		return
	}

	addr = u.Host
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	return
}
