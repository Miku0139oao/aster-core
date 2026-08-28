package http

import (
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	C "github.com/Miku0139oao/aster-core/constant"
	authStore "github.com/Miku0139oao/aster-core/listener/auth"
)

type captureTunnel struct {
	metadata *C.Metadata
}

func (t *captureTunnel) HandleTCPConn(_ net.Conn, metadata *C.Metadata) {
	t.metadata = metadata
}
func (t *captureTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (t *captureTunnel) NatTable() C.NatTable                     { return nil }

func TestHTTPConnectHandshakeSetsUserAndReply(t *testing.T) {
	req := []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic dXNlcjpwYXNz\r\n\r\n")
	var reply bytes.Buffer
	conn := &benchConn{
		Reader: bytes.NewReader(req),
		Writer: &reply,
		local:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080},
		remote: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
	}
	tunnel := &captureTunnel{}
	HandleConn(conn, tunnel, authStore.Nil, inbound.WithInName("http-in"))

	if got, want := reply.String(), "HTTP/1.1 200 Connection established\r\n\r\n"; got != want {
		t.Fatalf("unexpected CONNECT reply:\n got %q\nwant %q", got, want)
	}
	if tunnel.metadata == nil {
		t.Fatal("missing metadata")
	}
	if tunnel.metadata.InUser != "user" {
		t.Fatalf("InUser = %q, want user", tunnel.metadata.InUser)
	}
	if tunnel.metadata.Host != "example.com" || tunnel.metadata.DstPort != 443 {
		t.Fatalf("unexpected destination: host=%q port=%d", tunnel.metadata.Host, tunnel.metadata.DstPort)
	}
	if tunnel.metadata.InName != "http-in" {
		t.Fatalf("InName = %q", tunnel.metadata.InName)
	}
}

func TestIsProxyKeepAlive(t *testing.T) {
	if !isProxyKeepAlive("Keep-Alive") || !isProxyKeepAlive(" keep-alive ") {
		t.Fatal("expected keep-alive")
	}
	if isProxyKeepAlive("close") || isProxyKeepAlive("") {
		t.Fatal("did not expect keep-alive")
	}
}

func TestWriteConnectEstablishedHTTP10(t *testing.T) {
	var buf bytes.Buffer
	if err := writeConnectEstablished(&buf, 1, 0); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "HTTP/1.0 200 Connection established\r\n\r\n" {
		t.Fatalf("unexpected reply: %q", buf.String())
	}
	if err := writeConnectEstablished(io.Discard, 1, 1); err != nil {
		t.Fatal(err)
	}
}
