package http

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	authStore "github.com/Miku0139oao/aster-core/listener/auth"
)

type benchConn struct {
	io.Reader
	io.Writer
	local  net.Addr
	remote net.Addr
}

func (c *benchConn) Close() error                     { return nil }
func (c *benchConn) LocalAddr() net.Addr              { return c.local }
func (c *benchConn) RemoteAddr() net.Addr             { return c.remote }
func (c *benchConn) SetDeadline(time.Time) error      { return nil }
func (c *benchConn) SetReadDeadline(time.Time) error  { return nil }
func (c *benchConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkHTTPConnectHandshake(b *testing.B) {
	req := []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
	remote := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	additions := []inbound.Addition{inbound.WithInName("bench-http")}
	reader := bytes.NewReader(req)
	conn := &benchConn{
		Writer: io.Discard,
		local:  local,
		remote: remote,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(req)
		conn.Reader = reader
		HandleConn(conn, stubTunnel{}, authStore.Nil, additions...)
	}
}
