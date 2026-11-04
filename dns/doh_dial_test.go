package dns

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingDialer is a dial function that never returns on its own.  It only
// stops when the context it is given is cancelled, which lets a test prove
// that the caller's deadline is actually propagated into the dial.
type blockingDialer struct {
	returned atomic.Bool
}

func (d *blockingDialer) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	<-ctx.Done()
	d.returned.Store(true)

	return nil, ctx.Err()
}

// TestBoundDialBounded checks that a dial which would block forever is cut off
// by the bound instead of running until the OS TCP timeout.  The timeout is a
// parameter here, so the test does not wait the production 10s.
func TestBoundDialBounded(t *testing.T) {
	const shortTimeout = 50 * time.Millisecond

	dialer := &blockingDialer{}
	start := time.Now()
	conn, err := boundDial(context.Background(), shortTimeout, dialer.dial, "tcp", "127.0.0.1:443")
	elapsed := time.Since(start)

	assert.Nil(t, conn)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, dialer.returned.Load(), "the dial should have observed cancellation")
	assert.GreaterOrEqual(t, elapsed, shortTimeout)
	assert.Less(t, elapsed, shortTimeout+2*time.Second,
		"dial returned after %s, bound was %s", elapsed, shortTimeout)
}

// TestBoundDialPropagatesCallerDeadline checks that an earlier caller deadline
// still wins over the bound and is not extended by it.
func TestBoundDialPropagatesCallerDeadline(t *testing.T) {
	const callerTimeout = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), callerTimeout)
	defer cancel()

	dialer := &blockingDialer{}
	start := time.Now()
	_, err := boundDial(ctx, time.Hour, dialer.dial, "tcp", "127.0.0.1:443")

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "caller deadline should not be extended")
}

// TestBoundDialContextIsBounded checks the wrapper that createTransport
// installs: a dial which never returns is cut off at the configured bound.
// This is the behaviour that stops abandoned dials from holding a
// MaxConnsPerHost slot until the OS TCP timeout.
func TestBoundDialContextIsBounded(t *testing.T) {
	const override = 40 * time.Millisecond

	dialer := &blockingDialer{}
	dial := boundDialContext(override, dialer.dial)

	start := time.Now()
	_, err := dial(context.Background(), "tcp", "doh.example:80")
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, dialer.returned.Load())
	assert.GreaterOrEqual(t, elapsed, override)
	assert.Less(t, elapsed, override+2*time.Second)
}

// TestCreateTransportH1TransportLimits checks the remaining H1/H2 transport
// settings that interact with the dial bound: the TLS handshake is bounded too
// (otherwise a peer that accepts TCP and then stalls still holds a connection
// slot), and the per-host connection ceiling is still in place.
func TestCreateTransportH1TransportLimits(t *testing.T) {
	doh := &dnsOverHTTPS{
		url: &url.URL{Scheme: "https", Host: "doh.example:443"},
	}

	rt, err := doh.createTransport(context.Background())
	require.NoError(t, err)

	transport, ok := rt.(*http.Transport)
	require.True(t, ok, "expected a plain HTTP transport, got %T", rt)
	assert.Equal(t, dialTimeout, transport.TLSHandshakeTimeout,
		"TLS handshakes must be bounded too, or they outlive the dial bound")
	assert.Equal(t, dohMaxConnsPerHost, transport.MaxConnsPerHost)
	assert.Equal(t, dohMaxIdleConns, transport.MaxIdleConns)
	assert.True(t, transport.ForceAttemptHTTP2)
}

// lazyConn is a dialer that returns a conn before the TCP connect happens and
// finishes it on first Write, holding the context it was given. TFO and the
// early-data websocket path behave this way. If the bound cancels the dial
// context as soon as the dial returns, that deferred connect fails.
type lazyConn struct {
	ctx      context.Context
	connDone bool
}

func (c *lazyConn) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	c.connDone = true
	return len(p), nil
}

func (c *lazyConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (c *lazyConn) Close() error                     { return nil }
func (c *lazyConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *lazyConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *lazyConn) SetDeadline(time.Time) error      { return nil }
func (c *lazyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *lazyConn) SetWriteDeadline(time.Time) error { return nil }

// TestBoundDialKeepsContextAliveForLazyDialers guards the regression where
// boundDial ran `defer cancel()` on the success path, killing the dial context
// that a lazily-connecting dialer still needs.
func TestBoundDialKeepsContextAliveForLazyDialers(t *testing.T) {
	var lazy *lazyConn
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		lazy = &lazyConn{ctx: ctx}
		return lazy, nil
	}

	conn, err := boundDial(context.Background(), time.Hour, dial, "tcp", "doh.example:443")
	require.NoError(t, err)
	defer conn.Close()

	if _, err := conn.Write([]byte("clienthello")); err != nil {
		t.Fatalf("lazy dialer could not complete its connect after boundDial returned: %v", err)
	}
	require.True(t, lazy.connDone)
}

// TestBoundDialCancelsOnError keeps the other half: a failed dial must not
// leave the timeout goroutine running.
func TestBoundDialCancelsOnError(t *testing.T) {
	deadlineSeen := make(chan struct{})
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		go func() {
			<-ctx.Done()
			close(deadlineSeen)
		}()
		return nil, errors.New("dial failed")
	}

	_, err := boundDial(context.Background(), time.Hour, dial, "tcp", "doh.example:443")
	require.Error(t, err)

	select {
	case <-deadlineSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("boundDial left its context alive after a failed dial")
	}
}
