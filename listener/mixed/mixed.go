package mixed

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/component/auth"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	authStore "github.com/Miku0139oao/aster-core/listener/auth"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/http"
	"github.com/Miku0139oao/aster-core/listener/reality"
	"github.com/Miku0139oao/aster-core/listener/socks"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/transport/socks4"
	"github.com/Miku0139oao/aster-core/transport/socks5"

	"github.com/metacubex/tls"
)

type Listener struct {
	listener net.Listener
	addr     string
	closed   atomic.Bool
}

// RawAddress implements C.Listener
func (l *Listener) RawAddress() string {
	return l.addr
}

// Address implements C.Listener
func (l *Listener) Address() string {
	return l.listener.Addr().String()
}

// Close implements C.Listener
func (l *Listener) Close() error {
	l.closed.Store(true)
	return l.listener.Close()
}

func New(addr string, tunnel C.Tunnel, additions ...inbound.Addition) (*Listener, error) {
	return NewWithConfig(LC.AuthServer{Enable: true, Listen: addr, AuthStore: authStore.Default}, inbound.NewListenConfig(), tunnel, additions...)
}

func NewWithConfig(config LC.AuthServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (*Listener, error) {
	isDefault := false
	if len(additions) == 0 {
		isDefault = true
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-MIXED"),
			inbound.WithSpecialRules(""),
		}
	}

	l, err := lc.Listen(context.Background(), "tcp", config.Listen)
	if err != nil {
		return nil, err
	}
	keepListener := false
	defer func() {
		if !keepListener {
			_ = l.Close()
		}
	}()

	tlsConfig := &tls.Config{Time: ntp.Now}
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
	if config.RealityConfig.PrivateKey != "" {
		if tlsConfig.GetCertificate != nil {
			return nil, errors.New("certificate is unavailable in reality")
		}
		if tlsConfig.ClientAuth != tls.NoClientCert {
			return nil, errors.New("client-auth is unavailable in reality")
		}
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}

	if realityBuilder != nil {
		l = realityBuilder.NewListener(l)
	} else if tlsConfig.GetCertificate != nil {
		l = tls.NewListener(l, tlsConfig)
	}

	ml := &Listener{
		listener: l,
		addr:     config.Listen,
	}
	keepListener = true
	go func() {
		for {
			c, err := ml.listener.Accept()
			if err != nil {
				if ml.closed.Load() {
					break
				}
				continue
			}
			store := config.AuthStore
			if isDefault || store == authStore.Default { // only apply on default listener
				if !inbound.IsRemoteAddrDisAllowed(c.RemoteAddr()) {
					_ = c.Close()
					continue
				}
				if inbound.SkipAuthRemoteAddr(c.RemoteAddr()) {
					store = authStore.Nil
				}
			}
			go handleConn(c, tunnel, store, additions...)
		}
	}()

	return ml, nil
}

type prefixConn struct {
	net.Conn
	head byte
	read bool
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if c.read {
		return c.Conn.Read(p)
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = c.head
	c.read = true
	if len(p) == 1 {
		return 1, nil
	}
	n, err := c.Conn.Read(p[1:])
	return 1 + n, err
}

func handleConn(conn net.Conn, tunnel C.Tunnel, store auth.AuthStore, additions ...inbound.Addition) {
	var head [1]byte
	_, err := io.ReadFull(conn, head[:])
	if err != nil {
		_ = conn.Close()
		return
	}

	prefixed := &prefixConn{Conn: conn, head: head[0]}
	switch head[0] {
	case socks4.Version:
		socks.HandleSocks4(prefixed, tunnel, store, additions...)
	case socks5.Version:
		socks.HandleSocks5(prefixed, tunnel, store, additions...)
	default:
		http.HandleConn(prefixed, tunnel, store, additions...)
	}
}
