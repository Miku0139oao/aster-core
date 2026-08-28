package socks

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

type discardPacketConn struct{}

func (discardPacketConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, io.EOF }
func (discardPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return len(b), nil
}
func (discardPacketConn) Close() error { return nil }
func (discardPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}
}
func (discardPacketConn) SetDeadline(time.Time) error      { return nil }
func (discardPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (discardPacketConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkHandleSocks5(b *testing.B) {
	req := []byte{
		0x05, 0x01, 0x00, // no-auth
		0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0x00, 0x50, // CONNECT 1.2.3.4:80
	}
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}
	remote := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	additions := []inbound.Addition{inbound.WithInName("bench-socks")}
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
		HandleSocks5(conn, stubTunnel{}, authStore.Nil, additions...)
	}
}

func BenchmarkSocksUDPWriteBack(b *testing.B) {
	pkt := &packet{
		pc:    discardPacketConn{},
		rAddr: &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
	}
	payload := make([]byte, 64)
	src := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 1), Port: 443}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pkt.WriteBack(payload, src); err != nil {
			b.Fatal(err)
		}
	}
}
