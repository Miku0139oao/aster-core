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
	connections deque.Deque[net.Conn] // LIFO
}

var _ dnsClient = (*dnsOverTLS)(nil)

// Address implements dnsClient
func (t *dnsOverTLS) Address() string {
	return fmt.Sprintf("tls://%s", net.JoinHostPort(t.host, t.port))
}

func (t *dnsOverTLS) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	// miekg/dns ExchangeContext doesn't respond to context cancel.
	// this is a workaround
	type result struct {
		msg *D.Msg
		err error
	}
	ch := make(chan result, 1)

	go func() {
		var msg *D.Msg
		var err error
		defer func() { ch <- result{msg, err} }()
		for { // retry loop; only retry when reusing old conn
			err = ctx.Err() // check context first
			if err != nil {
				return
			}

			var conn net.Conn
			isOldConn := true

			if !t.disableReuse {
				t.access.Lock()
				if t.connections.Len() > 0 {
					conn = t.connections.PopBack()
				}
				t.access.Unlock()
			}

			if conn == nil {
				conn, err = t.dialContext(ctx)
				if err != nil {
					return
				}
				isOldConn = false
			}

			msg, err = exchangeLengthPrefixedConn(conn, m, 5*time.Second)
			if err != nil {
				_ = conn.Close()
				conn = nil
				if isOldConn { // retry
					continue
				}
				return
			}

			if !t.disableReuse {
				t.access.Lock()
				if t.connections.Len() >= maxOldDotConns {
					oldConn := t.connections.PopFront()
					go oldConn.Close() // close in a new goroutine, not blocking the current task
				}
				t.connections.PushBack(conn)
				t.access.Unlock()
			} else {
				_ = conn.Close()
			}
			return
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ret := <-ch:
		return ret.msg, ret.err
	}
}

func exchangeLengthPrefixedConn(conn net.Conn, m *D.Msg, timeout time.Duration) (*D.Msg, error) {
	if timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
		defer conn.SetDeadline(time.Time{})
	}

	buf := pool.Get(2 + MaxMsgSize)
	defer pool.Put(buf)

	packed, err := m.PackBuffer(buf[2:])
	if err != nil {
		return nil, err
	}
	if err = putLengthPrefixed(buf, packed); err != nil {
		return nil, err
	}
	if _, err = conn.Write(buf[:2+len(packed)]); err != nil {
		return nil, err
	}

	if _, err = io.ReadFull(conn, buf[:2]); err != nil {
		return nil, err
	}
	respLen := binary.BigEndian.Uint16(buf[:2])
	if respLen == 0 {
		return nil, fmt.Errorf("received empty DNS response")
	}
	if int(respLen) > MaxMsgSize {
		return nil, fmt.Errorf("received response that is too large: %d > %d", respLen, MaxMsgSize)
	}
	if _, err = io.ReadFull(conn, buf[:respLen]); err != nil {
		return nil, err
	}

	resp := new(D.Msg)
	if err = resp.Unpack(buf[:respLen]); err != nil {
		return nil, err
	}
	if resp.Id != m.Id {
		return resp, D.ErrId
	}
	return resp, nil
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
	if !t.disableReuse {
		t.access.Lock()
		for t.connections.Len() > 0 {
			oldConn := t.connections.PopFront()
			go oldConn.Close() // close in a new goroutine, not blocking the current task
		}
		t.access.Unlock()
	}
}

func (t *dnsOverTLS) Close() error {
	runtime.SetFinalizer(t, nil)
	t.ResetConnection()
	return nil
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
