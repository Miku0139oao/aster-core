package packet

import (
	"net"
	"testing"
	"time"

	"github.com/metacubex/sing/common/buf"
	M "github.com/metacubex/sing/common/metadata"
)

type singPacketConnStub struct {
	payload     []byte
	destination M.Socksaddr
}

func (c *singPacketConnStub) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	if _, err := buffer.Write(c.payload); err != nil {
		return M.Socksaddr{}, err
	}
	return c.destination, nil
}

func (*singPacketConnStub) WritePacket(*buf.Buffer, M.Socksaddr) error { return nil }
func (*singPacketConnStub) ReadFrom([]byte) (int, net.Addr, error)     { return 0, nil, net.ErrClosed }
func (*singPacketConnStub) WriteTo(p []byte, _ net.Addr) (int, error)  { return len(p), nil }
func (*singPacketConnStub) Close() error                               { return nil }
func (*singPacketConnStub) LocalAddr() net.Addr                        { return &net.UDPAddr{} }
func (*singPacketConnStub) SetDeadline(time.Time) error                { return nil }
func (*singPacketConnStub) SetReadDeadline(time.Time) error            { return nil }
func (*singPacketConnStub) SetWriteDeadline(time.Time) error           { return nil }

func TestEnhanceSingPacketConnWaitReadFrom(t *testing.T) {
	stub := &singPacketConnStub{
		payload:     []byte("sing-wait-read"),
		destination: M.ParseSocksaddr("198.51.100.9:53"),
	}
	conn := NewEnhancePacketConn(stub)
	data, put, addr, err := conn.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put != nil {
		defer put()
	}
	if string(data) != string(stub.payload) {
		t.Fatalf("payload = %q, want %q", data, stub.payload)
	}
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("addr type = %T, want *net.UDPAddr", addr)
	}
	if udpAddr.Port != 53 {
		t.Fatalf("port = %d, want 53", udpAddr.Port)
	}
}

func BenchmarkEnhanceSingPacketConnWaitReadFrom(b *testing.B) {
	stub := &singPacketConnStub{
		payload:     []byte("sing-wait-read"),
		destination: M.ParseSocksaddr("198.51.100.9:53"),
	}
	conn := NewEnhancePacketConn(stub)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, put, addr, err := conn.WaitReadFrom()
		if err != nil {
			b.Fatal(err)
		}
		if put != nil {
			put()
		}
		if addr == nil || len(data) == 0 {
			b.Fatal("empty read")
		}
	}
}
