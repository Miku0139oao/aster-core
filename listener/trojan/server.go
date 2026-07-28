package trojan

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/utils"
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
	"github.com/Miku0139oao/aster-core/transport/shadowsocks/core"
	"github.com/Miku0139oao/aster-core/transport/socks5"
	"github.com/Miku0139oao/aster-core/transport/trojan"
	mihomoVMess "github.com/Miku0139oao/aster-core/transport/vmess"

	"github.com/metacubex/http"
	"github.com/metacubex/smux"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed     atomic.Bool
	config     LC.TrojanServer
	listeners  []net.Listener
	httpServer *http.Server
	keys       map[[trojan.KeyLength]byte]string
	pickCipher core.Cipher
	handler    *sing.ListenerHandler
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

func New(config LC.TrojanServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-TROJAN"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.TROJAN,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}

	keys := make(map[[trojan.KeyLength]byte]string)
	for _, user := range config.Users {
		keys[trojan.Key(user.Password)] = user.Username
	}

	var pickCipher core.Cipher
	if config.TrojanSSOption.Enabled {
		if config.TrojanSSOption.Password == "" {
			return nil, errors.New("empty password")
		}
		if config.TrojanSSOption.Method == "" {
			config.TrojanSSOption.Method = "AES-128-GCM"
		}
		pickCipher, err = core.PickCipher(config.TrojanSSOption.Method, nil, config.TrojanSSOption.Password)
		if err != nil {
			return nil, err
		}
	}
	sl = &Listener{config: config, keys: keys, pickCipher: pickCipher, handler: h}
	created := sl
	defer func() {
		if err != nil {
			if closeErr := created.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	httpServer := http.Server{
		IdleTimeout: 30 * time.Second,
		Protocols:   new(http.Protocols),
	}
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
	if httpServer.Handler != nil {
		sl.httpServer = &httpServer
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		//TCP
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		sl.listeners = append(sl.listeners, l)
		listenerIndex := len(sl.listeners) - 1
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
		} else if !config.TrojanSSOption.Enabled && !config.AllowInsecure {
			return nil, errors.New("disallow using Trojan without any certificates/shadow-tls/res-tls/jls/reality/ss/allow-insecure config")
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

				go created.HandleConn(c, tunnel, additions...)
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
	defer conn.Close()

	if l.pickCipher != nil {
		conn = l.pickCipher.StreamConn(conn)
	}

	var key [trojan.KeyLength]byte
	if _, err := io.ReadFull(conn, key[:]); err != nil {
		//log.Warnln("read key error: %s", err.Error())
		return
	}

	if user, ok := l.keys[key]; ok {
		additions = append(additions, inbound.WithInUser(user))
	} else {
		//log.Warnln("no such key")
		return
	}

	var crlf [2]byte
	if _, err := io.ReadFull(conn, crlf[:]); err != nil {
		//log.Warnln("read crlf error: %s", err.Error())
		return
	}

	l.handleConn(false, conn, tunnel, additions...)
}

func (l *Listener) handleConn(inMux bool, conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) {
	if inMux {
		defer conn.Close()
	}

	command, err := socks5.ReadByte(conn)
	if err != nil {
		//log.Warnln("read command error: %s", err.Error())
		return
	}

	switch command {
	case trojan.CommandTCP, trojan.CommandUDP, trojan.CommandMux:
	default:
		//log.Warnln("unknown command: %d", command)
		return
	}

	target, err := socks5.ReadAddr0(conn)
	if err != nil {
		//log.Warnln("read target error: %s", err.Error())
		return
	}

	if !inMux {
		var crlf [2]byte
		if _, err := io.ReadFull(conn, crlf[:]); err != nil {
			//log.Warnln("read crlf error: %s", err.Error())
			return
		}
	}

	switch command {
	case trojan.CommandTCP:
		//tunnel.HandleTCPConn(inbound.NewSocket(target, conn, C.TROJAN, additions...))
		l.handler.HandleSocket(target, conn, additions...)
	case trojan.CommandUDP:
		pc := trojan.NewPacketConn(conn)
		remoteAddr := conn.RemoteAddr()
		connID := utils.NewUUIDV4().String() // make a new SNAT key

		for {
			data, put, addr, err := pc.WaitReadFrom()
			if err != nil {
				if put != nil {
					put()
				}
				break
			}
			target := socks5.ParseAddrToSocksAddr(addr)
			cPacket := &packet{
				pc:      pc,
				rAddr:   remoteAddr,
				payload: data,
				put:     put,
			}
			cPacket.rAddr = N.NewCustomAddr(C.TROJAN.String(), connID, cPacket.rAddr) // for tunnel's handleUDPConn

			tunnel.HandleUDPPacket(inbound.NewPacket(target, cPacket, C.TROJAN, additions...))
		}
	case trojan.CommandMux:
		if inMux {
			//log.Warnln("invalid command: %d", command)
			return
		}
		smuxConfig := smux.DefaultConfig()
		smuxConfig.KeepAliveDisabled = true
		session, err := smux.Server(conn, smuxConfig)
		if err != nil {
			//log.Warnln("smux server error: %s", err.Error())
			return
		}
		defer session.Close()
		for {
			stream, err := session.AcceptStream()
			if err != nil {
				return
			}
			go l.handleConn(true, stream, tunnel, additions...)
		}
	}
}
