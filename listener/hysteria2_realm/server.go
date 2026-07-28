package hysteria2_realm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/ntp"

	"github.com/metacubex/http"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed    bool
	config    LC.Hysteria2RealmServer
	listeners []net.Listener
	http      []*http.Server
	server    *server
	cancel    func()
	closeOnce sync.Once
	closeErr  error
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

const (
	DefaultMaxRealms        = 65536
	DefaultMaxRealmsPerIP   = 4
	DefaultRealmNamePattern = defaultRealmNamePattern
)

func DefaultALPN() []string { return []string{"h2", "http/1.1"} }

func New(config LC.Hysteria2RealmServer, lc C.InboundListenConfig, tunnel C.Tunnel, _ ...inbound.Addition) (sl *Listener, err error) {
	pat, err := regexp.Compile(config.RealmNamePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid realm name pattern %q: %v", config.RealmNamePattern, err)
	}
	s := newServer(serverConfig{
		realmToken:     config.Token,
		maxRealms:      config.MaxRealms,
		maxRealmsPerIP: config.MaxRealmsPerIP,
		proxyHeader:    config.TrustedProxyHeader,
		realmIDPattern: pat,
	})

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

	sl = &Listener{config: config, server: s}
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

		// TCP
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		if tlsConfig.GetCertificate != nil {
			l = tls.NewListener(l, tlsConfig)
		}
		l = &closeOnceListener{Listener: l}
		sl.listeners = append(sl.listeners, l)

		srv := &http.Server{
			Handler:           s.routes(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		sl.http = append(sl.http, srv)

		go srv.Serve(l)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sl.cancel = cancel
	go s.reaper(ctx)

	return sl, nil
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closed = true
		for _, srv := range l.http {
			if err := srv.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		for _, lis := range l.listeners {
			if err := lis.Close(); err != nil {
				l.closeErr = errors.Join(l.closeErr, err)
			}
		}
		if l.cancel != nil {
			l.cancel()
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

func debugf(format string, v ...any) {
	log.Debugln("[RealmServer] "+format, v...)
}
