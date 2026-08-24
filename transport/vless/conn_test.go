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
