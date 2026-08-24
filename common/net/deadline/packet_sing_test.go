package deadline

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

func TestSingPacketDeadlineReadKeepsPipeBuffer(t *testing.T) {
	stub := &singPacketConnStub{
		payload:     []byte("deadline packet"),
		destination: M.ParseSocksaddr("198.51.100.1:53"),
	}
	conn := NewSingPacketConn(stub)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	buffer := buf.NewSize(64)
	defer buffer.Release()
	destination, err := conn.ReadPacket(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if destination != stub.destination {
		t.Fatalf("destination = %v, want %v", destination, stub.destination)
	}
	if got := string(buffer.Bytes()); got != string(stub.payload) {
		t.Fatalf("payload = %q, want %q", got, stub.payload)
	}
}
