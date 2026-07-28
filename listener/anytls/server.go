package anytls

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/buf"
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
	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
	"github.com/Miku0139oao/aster-core/transport/anytls/session"

	"github.com/metacubex/sing/common/auth"
	"github.com/metacubex/sing/common/bufio"
	M "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed      atomic.Bool
	config      LC.AnyTLSServer
	listeners   []net.Listener
	tlsConfig   *tls.Config
	usersMu     sync.Mutex
	users       atomic.Pointer[userSnapshot]
	padding     atomic.Pointer[padding.PaddingFactory]
	pending     sync.Map
	connections sync.Map
	transports  []*N.ConnectionTrackingListener
}

type userSnapshot struct {
	byPasswordHash map[[32]byte]string
}

func New(config LC.AnyTLSServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-ANYTLS"),
			inbound.WithSpecialRules(""),
		}
	}

	var shadowTLSBuilder *shadowtls.Builder
	var restlsBuilder *restls.Builder
	var jlsBuilder *jls.Builder
	var realityBuilder *reality.Builder
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
	if tlsConfig.ClientAuth != tls.NoClientCert && tlsConfig.GetCertificate == nil {
		return nil, errors.New("client-auth requires certificate")
	}
	securityModes := make([]string, 0, 5)
	if tlsConfig.GetCertificate != nil {
		securityModes = append(securityModes, "certificate")
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
	if config.RealityConfig.PrivateKey != "" {
		securityModes = append(securityModes, "reality")
	}
	if len(securityModes) > 1 {
		return nil, errors.New("security modes are mutually exclusive: " + strings.Join(securityModes, ", "))
	}
	if len(securityModes) == 0 && !config.AllowInsecure {
		return nil, errors.New("disallow using AnyTLS without certificates/shadow-tls/res-tls/jls/reality/allow-insecure config")
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
	if config.RealityConfig.PrivateKey != "" {
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}

	users, err := buildUserSnapshot(config.Users)
	if err != nil {
		return nil, err
	}
	config.Users = cloneUsers(config.Users)
	sl = &Listener{
		config:    config,
		tlsConfig: tlsConfig,
	}
	created := sl
	defer func() {
		if err != nil {
			_ = created.Close()
		}
	}()
	sl.users.Store(users)

	if len(config.PaddingScheme) > 0 {
		if !padding.UpdatePaddingScheme([]byte(config.PaddingScheme), &sl.padding) {
			return nil, errors.New("incorrect padding scheme format")
		}
	} else {
		padding.UpdatePaddingScheme(padding.DefaultPaddingScheme, &sl.padding)
	}

	// Using sing handler can automatically handle UoT
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.ANYTLS,
		Additions: additions,
	})
	if err != nil {
		return nil, err
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		//TCP
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
		sl.listeners = append(sl.listeners, l)

		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					if sl.closed.Load() {
						break
					}
					continue
				}
				go sl.HandleConn(c, h)
			}
		}()
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.usersMu.Lock()
	l.closed.Store(true)
	snapshot := &userSnapshot{byPasswordHash: map[[32]byte]string{}}
	l.users.Store(snapshot)
	pendingConnections := l.removePendingConnectionsLocked()
	activeConnections := l.removeInvalidConnectionsLocked(snapshot)
	l.usersMu.Unlock()
	closeAnyTLSConnections(pendingConnections)
	closeAnyTLSConnections(activeConnections)
	l.closeTransportConnections()
	var retErr error
	for _, lis := range l.listeners {
		err := lis.Close()
		if err != nil {
			retErr = err
		}
	}
	return retErr
}

func (l *Listener) UpdateUsers(users map[string]string) error {
	snapshot, err := buildUserSnapshot(users)
	if err != nil {
		return err
	}
	l.usersMu.Lock()
	if l.closed.Load() {
		l.usersMu.Unlock()
		return net.ErrClosed
	}
	l.users.Store(snapshot)
	pendingConnections := l.removePendingConnectionsLocked()
	invalidConnections := l.removeInvalidConnectionsLocked(snapshot)
	l.usersMu.Unlock()
	closeAnyTLSConnections(pendingConnections)
	closeAnyTLSConnections(invalidConnections)
	l.closeTransportConnections()
	return nil
}

func (l *Listener) closeTransportConnections() {
	for _, transport := range l.transports {
		transport.CloseConnections()
	}
}

type pendingConnection struct {
	conn net.Conn
}

type activeConnection struct {
	conn         net.Conn
	passwordHash [32]byte
	user         string
}

func (l *Listener) removeInvalidConnectionsLocked(snapshot *userSnapshot) (connections []net.Conn) {
	l.connections.Range(func(key, _ any) bool {
		active := key.(*activeConnection)
		if user, valid := snapshot.byPasswordHash[active.passwordHash]; valid && user == active.user {
			return true
		}
		l.connections.Delete(key)
		connections = append(connections, active.conn)
		return true
	})
	return connections
}

func (l *Listener) removePendingConnectionsLocked() (connections []net.Conn) {
	l.pending.Range(func(key, _ any) bool {
		l.pending.Delete(key)
		connections = append(connections, key.(*pendingConnection).conn)
		return true
	})
	return connections
}

func closeAnyTLSConnections(connections []net.Conn) {
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (l *Listener) trackPendingConnection(conn net.Conn) (*pendingConnection, bool) {
	l.usersMu.Lock()
	defer l.usersMu.Unlock()
	if l.closed.Load() {
		return nil, false
	}
	pending := &pendingConnection{conn: conn}
	l.pending.Store(pending, struct{}{})
	return pending, true
}

func (l *Listener) promoteConnection(pending *pendingConnection, passwordHash [32]byte, user string) (*activeConnection, bool) {
	l.usersMu.Lock()
	defer l.usersMu.Unlock()
	currentUser, valid := l.users.Load().byPasswordHash[passwordHash]
	_, pendingActive := l.pending.Load(pending)
	if l.closed.Load() || !pendingActive || !valid || currentUser != user {
		return nil, false
	}
	l.pending.Delete(pending)
	active := &activeConnection{conn: pending.conn, passwordHash: passwordHash, user: user}
	l.connections.Store(active, struct{}{})
	return active, true
}

func buildUserSnapshot(users map[string]string) (*userSnapshot, error) {
	byPasswordHash := make(map[[32]byte]string, len(users))
	for user, password := range users {
		hash := sha256.Sum256([]byte(password))
		if existing, exists := byPasswordHash[hash]; exists {
			return nil, errors.New("duplicate AnyTLS password for users " + existing + " and " + user)
		}
		byPasswordHash[hash] = user
	}
	return &userSnapshot{byPasswordHash: byPasswordHash}, nil
}

func cloneUsers(users map[string]string) map[string]string {
	cloned := make(map[string]string, len(users))
	for user, password := range users {
		cloned[user] = password
	}
	return cloned
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

func (l *Listener) HandleConn(conn net.Conn, h *sing.ListenerHandler) {
	ctx := context.TODO()
	rawConn := conn
	defer rawConn.Close()
	pending, tracked := l.trackPendingConnection(rawConn)
	if !tracked {
		return
	}
	defer l.pending.Delete(pending)

	b := buf.NewPacket()
	defer b.Release()

	_, err := b.ReadOnceFrom(conn)
	if err != nil {
		return
	}
	conn = bufio.NewCachedConn(conn, b)

	by, err := b.ReadBytes(32)
	if err != nil {
		return
	}
	var passwordSha256 [32]byte
	copy(passwordSha256[:], by)
	snapshot := l.users.Load()
	user, authenticated := snapshot.byPasswordHash[passwordSha256]
	if authenticated {
		ctx = auth.ContextWithUser(ctx, user)
	} else {
		return
	}
	by, err = b.ReadBytes(2)
	if err != nil {
		return
	}
	paddingLen := binary.BigEndian.Uint16(by)
	if paddingLen > 0 {
		_, err = b.ReadBytes(int(paddingLen))
		if err != nil {
			return
		}
	}
	active, promoted := l.promoteConnection(pending, passwordSha256, user)
	if !promoted {
		return
	}
	defer l.connections.Delete(active)

	session := session.NewServerSession(conn, func(stream *session.Stream) {
		defer stream.Close()

		destination, err := M.SocksaddrSerializer.ReadAddrPort(stream)
		if err != nil {
			return
		}

		// It seems that mihomo does not implement a connection error reporting mechanism, so we report success directly.
		err = stream.HandshakeSuccess()
		if err != nil {
			return
		}

		h.NewConnection(ctx, stream, M.Metadata{
			Source:      M.SocksaddrFromNet(conn.RemoteAddr()),
			Destination: destination,
		})
	}, &l.padding)
	session.Run()
	session.Close()
}
