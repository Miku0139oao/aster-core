package mixed

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

func TestMixedMuxDispatchesSOCKS5(t *testing.T) {
	req := []byte{
		0x05, 0x01, 0x00,
		0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0x00, 0x50,
	}
	conn := &benchConn{
		Reader: bytes.NewReader(req),
		Writer: io.Discard,
		local:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7890},
		remote: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
	}
	handleConn(conn, stubTunnel{}, authStore.Nil, inbound.WithInName("mixed-in"))
}

func TestPrefixConnReplaysFirstByte(t *testing.T) {
	// handleConn consumes the first byte, then prefixConn must replay it
	// in front of the remaining stream.
	inner := bytes.NewReader([]byte{0x01, 0x00})
	c := &prefixConn{Conn: &benchConn{Reader: inner, Writer: io.Discard}, head: 0x05}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, []byte{0x05, 0x01, 0x00}) {
		t.Fatalf("got %v", buf)
	}
}

func BenchmarkMixedMuxHTTPConnect(b *testing.B) {
	req := []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7890}
	remote := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	additions := []inbound.Addition{inbound.WithInName("bench-mixed")}
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
		handleConn(conn, stubTunnel{}, authStore.Nil, additions...)
	}
}

func BenchmarkMixedMuxSOCKS5(b *testing.B) {
	req := []byte{
		0x05, 0x01, 0x00,
		0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0x00, 0x50,
	}
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7890}
	remote := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	additions := []inbound.Addition{inbound.WithInName("bench-mixed")}
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
		handleConn(conn, stubTunnel{}, authStore.Nil, additions...)
	}
}
