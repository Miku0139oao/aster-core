package trusttunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/httputils"
	"github.com/Miku0139oao/aster-core/common/once"
	"github.com/Miku0139oao/aster-core/component/dialer"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/vmess"

	"github.com/metacubex/http"
	"golang.org/x/exp/slices"
)

type DialOptionsFunc func() []dialer.Option

type ClientOptions struct {
	Dialer                C.Dialer
	DialOptions           DialOptionsFunc // for quic
	Server                string
	Username              string
	Password              string
	TLSConfig             *vmess.TLSConfig
	QUIC                  bool
	QUICCongestionControl string
	QUICCwnd              int
	QUICBBRProfile        string
	HealthCheck           bool
	MaxConnections        int
	MinStreams            int
	MaxStreams            int
}

type Client struct {
	ctx              context.Context
	cancel           context.CancelFunc
	dialer           C.Dialer
	dialOptions      DialOptionsFunc
	server           string
	auth             string
	roundTripper     http.RoundTripper
	startOnce        sync.Once
	healthCheck      bool
	healthCheckTimer *time.Timer
	healthCheckDone  chan struct{}
	healthCheckMu    sync.Mutex
	closed           atomic.Bool
	count            atomic.Int64
}

func NewClient(ctx context.Context, options ClientOptions) (client *Client, err error) {
	clientCtx, cancel := context.WithCancel(ctx)
	client = &Client{
		ctx:         clientCtx,
		cancel:      cancel,
		dialer:      options.Dialer,
		dialOptions: options.DialOptions,
		server:      options.Server,
		auth:        buildAuth(options.Username, options.Password),
	}
	defer func() {
		if err != nil {
			cancel()
		}
	}()
	if options.QUIC {
		if len(options.TLSConfig.NextProtos) == 0 {
			options.TLSConfig.NextProtos = []string{"h3"}
		} else if !slices.Contains(options.TLSConfig.NextProtos, "h3") {
			return nil, errors.New("require alpn h3")
		}
		err = client.quicRoundTripper(options.TLSConfig, options.QUICCongestionControl, options.QUICCwnd, options.QUICBBRProfile)
		if err != nil {
			return nil, err
		}
	} else {
		if len(options.TLSConfig.NextProtos) == 0 {
			options.TLSConfig.NextProtos = []string{"h2"}
		} else if !slices.Contains(options.TLSConfig.NextProtos, "h2") {
			return nil, errors.New("require alpn h2")
		}
		client.h2RoundTripper(options.TLSConfig)
	}
	if options.HealthCheck {
		client.healthCheck = true
	}
	return client, nil
}

func (c *Client) h2RoundTripper(tlsConfig *vmess.TLSConfig) {
	// use h2c mode to disallow the net/http fallback to http1.1
	//
	// Note that this usage is only applicable to our own net/http fork.
	// The standard library also needs to mask the tls.Conn type for the conn returned by DialTLSContext,
	// see: https://github.com/golang/go/issues/79293#issuecomment-4426393534
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	c.roundTripper = &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := c.dialer.DialContext(ctx, network, c.server)
			if err != nil {
				return nil, err
			}
			tlsConn, err := vmess.StreamTLSConn(ctx, conn, tlsConfig)
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
		Protocols:       protocols,
		IdleConnTimeout: DefaultSessionTimeout,
	}
}

func (c *Client) start() {
	if c.healthCheck && !c.closed.Load() {
		c.healthCheckMu.Lock()
		c.healthCheckTimer = time.NewTimer(DefaultHealthCheckTimeout)
		c.healthCheckDone = make(chan struct{})
		c.healthCheckMu.Unlock()
		go c.loopHealthCheck()
	}
}

func (c *Client) loopHealthCheck() {
	defer func() {
		c.healthCheckMu.Lock()
		if c.healthCheckDone != nil {
			close(c.healthCheckDone)
		}
		c.healthCheckMu.Unlock()
	}()
	for {
		select {
		case <-c.healthCheckTimer.C:
		case <-c.ctx.Done():
			c.healthCheckTimer.Stop()
			return
		}
		ctx, cancel := context.WithTimeout(c.ctx, DefaultHealthCheckTimeout)
		_ = c.HealthCheck(ctx)
		cancel()
	}
}

func (c *Client) resetHealthCheckTimer() {
	c.healthCheckMu.Lock()
	defer c.healthCheckMu.Unlock()
	if c.healthCheckTimer == nil || c.ctx.Err() != nil {
		return
	}
	if !c.healthCheckTimer.Stop() {
		select {
		case <-c.healthCheckTimer.C:
		default:
		}
	}
	c.healthCheckTimer.Reset(DefaultHealthCheckTimeout)
}

func (c *Client) roundTrip(request *http.Request, conn *httpConn) {
	pipeReader, pipeWriter := io.Pipe()
	request.Body = pipeReader
	*conn = httpConn{
		writer:  pipeWriter,
		created: make(chan struct{}),
	}
	if c.closed.Load() {
		_ = pipeReader.CloseWithError(net.ErrClosed)
		_ = pipeWriter.CloseWithError(net.ErrClosed)
		conn.setup(nil, net.ErrClosed)
		return
	}
	c.startOnce.Do(c.start)
	c.count.Add(1)
	conn.closeFn = once.OnceFunc(func() {
		c.count.Add(-1)
	})
	ctx, cancel := context.WithCancel(c.ctx) // requestCtx must alive during conn not closed
	conn.cancelFn = cancel                   // cancel ctx when conn closed
	go func() {
		timeout := time.AfterFunc(C.DefaultTCPTimeout, cancel) // only cancel when RoundTrip timeout
		defer timeout.Stop()                                   // RoundTrip already returned, stop the timer
		request = request.WithContext(httputils.NewAddrContext(&conn.NetAddr, ctx))
		response, err := c.roundTripper.RoundTrip(request)
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			_ = pipeReader.CloseWithError(err)
			conn.setup(nil, err)
		} else if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			err = fmt.Errorf("unexpected status code: %d", response.StatusCode)
			_ = pipeWriter.CloseWithError(err)
			_ = pipeReader.CloseWithError(err)
			conn.setup(nil, err)
		} else {
			c.resetHealthCheckTimer()
			conn.setup(response.Body, nil)
		}
	}()
}

func (c *Client) newConnectRequest(host, userAgent string) *http.Request {
	request := &http.Request{
		Method: http.MethodConnect,
		URL: &url.URL{
			Scheme: "https",
			Host:   c.server, // Use the proxy server authority so the pool keys reuse against the actual proxy endpoint.
		},
		Header: make(http.Header),
		Host:   host, // Send the actual CONNECT target as the Host header (:authority).
	}
	request.Header.Add("User-Agent", userAgent)
	request.Header.Add("Proxy-Authorization", c.auth)
	return request
}

func (c *Client) Dial(ctx context.Context, host string) (net.Conn, error) {
	request := c.newConnectRequest(host, TCPUserAgent)
	conn := &tcpConn{}
	c.roundTrip(request, &conn.httpConn)
	return conn, nil
}

func (c *Client) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	request := c.newConnectRequest(UDPMagicAddress, UDPUserAgent)
	conn := &clientPacketConn{}
	c.roundTrip(request, &conn.httpConn)
	return conn, nil
}

func (c *Client) ListenICMP(ctx context.Context) (*IcmpConn, error) {
	request := c.newConnectRequest(ICMPMagicAddress, ICMPUserAgent)
	conn := &IcmpConn{}
	c.roundTrip(request, &conn.httpConn)
	return conn, nil
}

func (c *Client) Close() error {
	c.closed.Store(true)
	c.cancel()
	httputils.CloseTransport(c.roundTripper)
	c.healthCheckMu.Lock()
	if c.healthCheckTimer != nil {
		c.healthCheckTimer.Stop()
	}
	done := c.healthCheckDone
	c.healthCheckMu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}

func (c *Client) ResetConnections() {
	httputils.CloseTransport(c.roundTripper)
	c.resetHealthCheckTimer()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	defer c.resetHealthCheckTimer()
	request := c.newConnectRequest(HealthCheckMagicAddress, HealthCheckUserAgent)
	response, err := c.roundTripper.RoundTrip(request.WithContext(ctx))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}
	return nil
}

type PoolClient struct {
	mutex          sync.Mutex
	closed         bool
	maxConnections int
	minStreams     int
	maxStreams     int
	ctx            context.Context
	options        ClientOptions
	clients        []*Client
}

func NewPoolClient(ctx context.Context, options ClientOptions) (*PoolClient, error) {
	maxConnections := options.MaxConnections
	minStreams := options.MinStreams
	maxStreams := options.MaxStreams
	if maxConnections == 0 && minStreams == 0 && maxStreams == 0 {
		maxConnections = 8
		minStreams = 5
	}
	client, err := NewClient(ctx, options) // reserve one client and verify the configuration
	if err != nil {
		return nil, err
	}
	return &PoolClient{
		maxConnections: maxConnections,
		minStreams:     minStreams,
		maxStreams:     maxStreams,
		ctx:            ctx,
		options:        options,
		clients:        []*Client{client},
	}, nil
}

func (c *PoolClient) Dial(ctx context.Context, host string) (net.Conn, error) {
	transport, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return transport.Dial(ctx, host)
}

func (c *PoolClient) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	transport, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return transport.ListenPacket(ctx)
}

func (c *PoolClient) ListenICMP(ctx context.Context) (*IcmpConn, error) {
	transport, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return transport.ListenICMP(ctx)
}

func (c *PoolClient) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for _, t := range c.clients {
		if err := t.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	c.clients = nil
	return errors.Join(errs...)
}

func (c *PoolClient) getClient() (*Client, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	var transport *Client
	for _, t := range c.clients {
		if transport == nil || t.count.Load() < transport.count.Load() {
			transport = t
		}
	}
	if transport == nil {
		return c.newTransportLocked()
	}
	numStreams := int(transport.count.Load())
	if numStreams == 0 {
		return transport, nil
	}
	if c.maxConnections > 0 {
		if len(c.clients) >= c.maxConnections || numStreams < c.minStreams {
			return transport, nil
		}
	} else {
		if c.maxStreams > 0 && numStreams < c.maxStreams {
			return transport, nil
		}
	}
	return c.newTransportLocked()
}

func (c *PoolClient) newTransportLocked() (*Client, error) {
	if c.closed {
		return nil, net.ErrClosed
	}
	transport, err := NewClient(c.ctx, c.options)
	if err != nil {
		return nil, err
	}
	c.clients = append(c.clients, transport)
	return transport, nil
}
