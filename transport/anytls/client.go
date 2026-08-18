package anytls

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/buf"
	"github.com/Miku0139oao/aster-core/transport/anytls/padding"
	"github.com/Miku0139oao/aster-core/transport/anytls/session"
	"github.com/Miku0139oao/aster-core/transport/vmess"

	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
)

type ClientConfig struct {
	Password                 string
	ClientMetadata           string
	IdleSessionCheckInterval time.Duration
	IdleSessionTimeout       time.Duration
	MinIdleSession           int
	DisableReuse             bool
	Server                   M.Socksaddr
	Dialer                   N.Dialer
	TLSConfig                *vmess.TLSConfig
}

type Client struct {
	passwordSha256 []byte
	tlsConfig      *vmess.TLSConfig
	dialer         N.Dialer
	server         M.Socksaddr
	sessionClient  *session.Client
	padding        atomic.Pointer[padding.PaddingFactory]
}

func NewClient(ctx context.Context, config ClientConfig) *Client {
	pw := sha256.Sum256([]byte(config.Password))
	c := &Client{
		passwordSha256: pw[:],
		tlsConfig:      config.TLSConfig,
		dialer:         config.Dialer,
		server:         config.Server,
	}
	// Initialize the padding state of this client
	padding.UpdatePaddingScheme(padding.DefaultPaddingScheme, &c.padding)
	c.sessionClient = session.NewClient(ctx, c.createOutboundTLSConnection, &c.padding, config.ClientMetadata, config.IdleSessionCheckInterval, config.IdleSessionTimeout, config.MinIdleSession, config.DisableReuse)
	return c
}

func (c *Client) CreateProxy(ctx context.Context, destination M.Socksaddr) (net.Conn, error) {
	conn, err := c.sessionClient.CreateStream(ctx)
	if err != nil {
		return nil, err
	}
	err = M.SocksaddrSerializer.WriteAddrPort(conn, destination)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *Client) createOutboundTLSConnection(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.server)
	if err != nil {
		return nil, err
	}

	paddingLen := initialPaddingLength(c.padding.Load())
	b, err := newAuthenticationPreamble(c.passwordSha256, paddingLen)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer b.Release()

	tlsConn, err := vmess.StreamTLSConn(ctx, conn, c.tlsConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}

	_, err = b.WriteTo(tlsConn)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

const maxAuthenticationPaddingLength = int(^uint16(0))

func newAuthenticationPreamble(password []byte, paddingLen int) (*buf.Buffer, error) {
	if paddingLen < 0 || paddingLen > maxAuthenticationPaddingLength {
		return nil, fmt.Errorf("AnyTLS authentication padding too large: %d", paddingLen)
	}

	b := buf.NewSize(len(password) + 2 + paddingLen)
	if _, err := b.Write(password); err != nil {
		b.Release()
		return nil, err
	}
	binary.BigEndian.PutUint16(b.Extend(2), uint16(paddingLen))
	if err := b.WriteZeroN(paddingLen); err != nil {
		b.Release()
		return nil, err
	}
	return b, nil
}

func initialPaddingLength(factory *padding.PaddingFactory) int {
	if factory == nil {
		return 0
	}
	if sizes := factory.GenerateRecordPayloadSizes(0); len(sizes) > 0 && sizes[0] > 0 {
		return sizes[0]
	}
	return 0
}

func (h *Client) Close() error {
	return h.sessionClient.Close()
}
