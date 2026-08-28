package socks

import (
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	C "github.com/Miku0139oao/aster-core/constant"
	authStore "github.com/Miku0139oao/aster-core/listener/auth"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

func TestAppendSocksUDPPacketMatchesLegacy(t *testing.T) {
	payload := []byte("hello-udp")
	cases := []net.Addr{
		&net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53},
		&net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443},
		&net.TCPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 853},
	}
	for _, addr := range cases {
		legacy, err := socks5.EncodeUDPPacket(socks5.ParseAddrToSocksAddr(addr), payload)
		if err != nil {
			t.Fatal(err)
		}
		got, err := appendSocksUDPPacket(make([]byte, 0, 64), addr, payload)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(legacy, got) {
			t.Fatalf("encode mismatch for %v\n legacy %v\n got    %v", addr, legacy, got)
		}
	}
}

type captureTunnel struct {
	metadata *C.Metadata
}

func (t *captureTunnel) HandleTCPConn(_ net.Conn, metadata *C.Metadata) {
	t.metadata = metadata
}
func (t *captureTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (t *captureTunnel) NatTable() C.NatTable                     { return nil }

func TestSocksUDPWriteBackOutlivesDrop(t *testing.T) {
	putCalls := 0
	pkt := &packet{
		pc:      discardPacketConn{},
		rAddr:   &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
		payload: []byte("payload"),
		put:     func() { putCalls++ },
	}
	pkt.Drop()
	if putCalls != 1 {
		t.Fatalf("put calls = %d", putCalls)
	}
	if _, err := pkt.WriteBack([]byte("x"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleSocks5SetsInUser(t *testing.T) {
	req := []byte{
		0x05, 0x01, 0x00,
		0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0x00, 0x50,
	}
	conn := &benchConn{
		Reader: bytes.NewReader(req),
		Writer: io.Discard,
		local:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		remote: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
	}
	tunnel := &captureTunnel{}
	HandleSocks5(conn, tunnel, authStore.Nil, inbound.WithInName("socks-in"))
	if tunnel.metadata == nil {
		t.Fatal("missing metadata")
	}
	if tunnel.metadata.InUser != "" {
		t.Fatalf("empty auth should leave InUser empty, got %q", tunnel.metadata.InUser)
	}
	if tunnel.metadata.DstIP.String() != "1.2.3.4" || tunnel.metadata.DstPort != 80 {
		t.Fatalf("unexpected dest %s:%d", tunnel.metadata.DstIP, tunnel.metadata.DstPort)
	}
}
