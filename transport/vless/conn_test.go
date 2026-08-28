package vless

import (
	"io"
	"net"
	"testing"

	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
)

func TestFirstWriteBufferReleasesTransferredBuffer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	payload := []byte("first payload")
	requestLen := 1 + 16 + 1 + 1 + 2 + 1 + 4 + len(payload)
	readDone := make(chan error, 1)
	go func() {
		_, err := io.CopyN(io.Discard, server, int64(requestLen))
		readDone <- err
	}()

	conn := &Conn{
		ExtendedConn: N.NewExtendedConn(client),
		dst: &DstAddr{
			AddrType: AtypIPv4,
			Addr:     []byte{192, 0, 2, 1},
			Port:     443,
		},
	}
	buffer := buf.NewSize(len(payload))
	if _, err := buffer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteBuffer(buffer); err != nil {
		t.Fatal(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if buffer.RawCap() != 0 {
		t.Fatalf("transferred buffer was not released: raw capacity=%d", buffer.RawCap())
	}
}

func TestRecvResponseReturnsTruncatedAddonError(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	conn := &Conn{ExtendedConn: N.NewExtendedConn(client)}
	go func() {
		_, _ = server.Write([]byte{Version, 2, 0x01})
		_ = server.Close()
	}()

	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected truncated VLESS response addon to fail")
	}
	if conn.received {
		t.Fatal("truncated response was marked received")
	}
}

type discardNetConn struct {
	net.Conn
}

func (discardNetConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardNetConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (discardNetConn) Close() error                { return nil }

func TestSendRequestRejectsOversizedAddons(t *testing.T) {
	conn := &Conn{
		ExtendedConn: N.NewExtendedConn(discardNetConn{}),
		addons:       &Addons{Seed: make([]byte, 300)},
		dst: &DstAddr{
			AddrType: AtypIPv4,
			Addr:     []byte{192, 0, 2, 1},
			Port:     443,
		},
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("expected oversized addons to fail")
	}
	if conn.sent {
		t.Fatal("failed request was marked sent")
	}
}

func BenchmarkSendRequest(b *testing.B) {
	payload := make([]byte, 512)
	conn := &Conn{
		ExtendedConn: N.NewExtendedConn(discardNetConn{}),
		id:           [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		addons:       &Addons{Flow: XRV},
		dst: &DstAddr{
			AddrType: AtypIPv4,
			Addr:     []byte{192, 0, 2, 1},
			Port:     443,
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.sent = false
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
