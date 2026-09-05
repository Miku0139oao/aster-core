package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/Miku0139oao/aster-core/common/contextutils"
	"github.com/Miku0139oao/aster-core/common/deque"
	"github.com/Miku0139oao/aster-core/common/pool"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/resolver"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/metacubex/tls"
	D "github.com/miekg/dns"
)

const maxOldDotConns = 8

type dnsOverTLS struct {
	port           string
	host           string
	dialer         *dnsDialer
	skipCertVerify bool
	nameCertVerify string
	disableReuse   bool

	access      sync.Mutex
	closed      bool
	connections deque.Deque[net.Conn] // LIFO
}

var _ dnsClient = (*dnsOverTLS)(nil)

// Address implements dnsClient
func (t *dnsOverTLS) Address() string {
	return fmt.Sprintf("tls://%s", net.JoinHostPort(t.host, t.port))
}

func (t *dnsOverTLS) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	for { // retry loop; only retry when reusing old conn
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var conn net.Conn
		isOldConn := false

		t.access.Lock()
		if t.closed {
			t.access.Unlock()
			return nil, net.ErrClosed
		}
		if !t.disableReuse && t.connections.Len() > 0 {
			conn = t.connections.PopBack()
			isOldConn = true
		}
		t.access.Unlock()

		if conn == nil {
			var err error
			conn, err = t.dialContext(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				t.access.Lock()
				closed := t.closed
				t.access.Unlock()
				if closed {
					return nil, net.ErrClosed
				}
				return nil, err
			}
			t.access.Lock()
			closed := t.closed
			t.access.Unlock()
			if closed {
				_ = conn.Close()
				return nil, net.ErrClosed
			}
		}

		msg, err := exchangeLengthPrefixedConnContext(ctx, conn, m, 5*time.Second)
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			t.access.Lock()
			closed := t.closed
			t.access.Unlock()
			if closed {
				return nil, net.ErrClosed
			}
			if isOldConn { // retry
				continue
			}
			return nil, err
		}

		if ctx.Err() != nil {
			_ = conn.Close()
			return nil, ctx.Err()
		}

		if t.disableReuse {
			_ = conn.Close()
			return msg, nil
		}

		t.access.Lock()
		if t.closed {
			t.access.Unlock()
			_ = conn.Close()
			return msg, nil
		}
		if t.connections.Len() >= maxOldDotConns {
			oldConn := t.connections.PopFront()
			go oldConn.Close() // close in a new goroutine, not blocking the current task
		}
		t.connections.PushBack(conn)
		t.access.Unlock()
		return msg, nil
	}
}

// exchangeLengthPrefixedConn is a Background wrapper around
// exchangeLengthPrefixedConnContext for identical-source helper benches/tests.
func exchangeLengthPrefixedConn(conn net.Conn, m *D.Msg, timeout time.Duration) (*D.Msg, error) {
	return exchangeLengthPrefixedConnContext(context.Background(), conn, m, timeout)
}

func exchangeLengthPrefixedConnContext(ctx context.Context, conn net.Conn, m *D.Msg, timeout time.Duration) (*D.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	applyDeadline := timeout > 0
	if deadline, ok := ctx.Deadline(); ok {
		d := time.Until(deadline)
		if d <= 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, context.DeadlineExceeded
		}
		if !applyDeadline || d < timeout {
			timeout = d
			applyDeadline = true
		}
	}

	// Install the exchange deadline before AfterFunc so a late cancel cannot
	// race with the initial SetDeadline and leave a now-deadline on a reused conn.
	if applyDeadline {
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
	}

	callbackDone := make(chan struct{})
	stop := contextutils.AfterFunc(ctx, func() {
		defer close(callbackDone)
		_ = conn.SetDeadline(time.Now()) // cancel any read or write operation on this conn
	})
	defer func() {
		// Stop does not join an already-running callback. Wait before clearing
		// the deadline so a late SetDeadline(now) cannot poison the next borrower.
		if !stop() {
			<-callbackDone
		}
		_ = conn.SetDeadline(time.Time{})
	}()

	buf := pool.Get(2 + MaxMsgSize)
	defer pool.Put(buf)

	packed, err := m.PackBuffer(buf[2:])
	if err != nil {
		return nil, errWithCtx(ctx, err)
	}
	if err = putLengthPrefixed(buf, packed); err != nil {
		return nil, errWithCtx(ctx, err)
	}
	if _, err = conn.Write(buf[:2+len(packed)]); err != nil {
		return nil, errWithCtx(ctx, err)
	}

	if _, err = io.ReadFull(conn, buf[:2]); err != nil {
		return nil, errWithCtx(ctx, err)
	}
	respLen := binary.BigEndian.Uint16(buf[:2])
	if respLen == 0 {
		return nil, errWithCtx(ctx, fmt.Errorf("received empty DNS response"))
	}
	if int(respLen) > MaxMsgSize {
		return nil, errWithCtx(ctx, fmt.Errorf("received response that is too large: %d > %d", respLen, MaxMsgSize))
	}
	if _, err = io.ReadFull(conn, buf[:respLen]); err != nil {
		return nil, errWithCtx(ctx, err)
	}

	resp := new(D.Msg)
	if err = resp.Unpack(buf[:respLen]); err != nil {
		return nil, errWithCtx(ctx, err)
	}
	if resp.Id != m.Id {
		return resp, D.ErrId
	}
	return resp, nil
}

func errWithCtx(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (t *dnsOverTLS) dialContext(ctx context.Context) (net.Conn, error) {
	conn, err := t.dialer.DialContext(ctx, "tcp", net.JoinHostPort(t.host, t.port))
	if err != nil {
		return nil, err
	}

	tlsConfig, err := ca.GetTLSConfig(ca.Option{
		TLSConfig: &tls.Config{
			ServerName:         t.host,
			InsecureSkipVerify: t.skipCertVerify,
		},
		NameCertVerify: t.nameCertVerify,
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	tlsConn := tls.Client(conn, tlsConfig)
	if err = tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	conn = tlsConn

	return conn, nil
}

func (t *dnsOverTLS) ResetConnection() {
	t.access.Lock()
	t.drainConnsLocked()
	t.access.Unlock()
}

func (t *dnsOverTLS) Close() error {
	runtime.SetFinalizer(t, nil)
	t.access.Lock()
	t.closed = true
	t.drainConnsLocked()
	t.access.Unlock()
	return nil
}

func (t *dnsOverTLS) drainConnsLocked() {
	if t.disableReuse {
		return
	}
	for t.connections.Len() > 0 {
		oldConn := t.connections.PopFront()
		go oldConn.Close() // close in a new goroutine, not blocking the current task
	}
}

func newDoTClient(addr string, resolver resolver.Resolver, params map[string]string, proxyAdapter C.ProxyAdapter, proxyName string) *dnsOverTLS {
	host, port, _ := net.SplitHostPort(addr)
	c := &dnsOverTLS{
		port:   port,
		host:   host,
		dialer: newDNSDialer(resolver, proxyAdapter, proxyName),
	}
	c.connections.SetBaseCap(maxOldDotConns)
	if params["skip-cert-verify"] == "true" {
		c.skipCertVerify = true
	}
	c.nameCertVerify = params["name-cert-verify"]
	if params["disable-reuse"] == "true" {
		c.disableReuse = true
	}
	runtime.SetFinalizer(c, (*dnsOverTLS).Close)
	return c
}
