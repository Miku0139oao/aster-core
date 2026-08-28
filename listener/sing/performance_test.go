package sing

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/metacubex/sing/common/buf"
	M "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/sing/common/network"
)

type stubTunnel struct{}

func (stubTunnel) HandleTCPConn(net.Conn, *C.Metadata)      {}
func (stubTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (stubTunnel) NatTable() C.NatTable                     { return nil }

var packetMetadataSink *C.Metadata

func TestPacketMetadataPoolClearsState(t *testing.T) {
	metadata := acquirePacketMetadata(C.TUN)
	metadata.Host = "example.com"
	metadata.InUser = "user"
	releasePacketMetadata(metadata)

	metadata = acquirePacketMetadata(C.SOCKS5)
	defer releasePacketMetadata(metadata)
	if metadata.NetWork != C.UDP || metadata.Type != C.SOCKS5 {
		t.Fatalf("unexpected base metadata: %#v", metadata)
	}
	if metadata.Host != "" || metadata.InUser != "" {
		t.Fatalf("pooled metadata retained state: %#v", metadata)
	}
}

func BenchmarkPacketMetadata(b *testing.B) {
	b.Run("allocate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			packetMetadataSink = &C.Metadata{NetWork: C.UDP, Type: C.TUN}
		}
	})
	b.Run("pool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			metadata := acquirePacketMetadata(C.TUN)
			packetMetadataSink = metadata
			releasePacketMetadata(metadata)
		}
	})
}

func TestHandlePacketSetsAddrsWithoutLeakingState(t *testing.T) {
	h := &ListenerHandler{ListenerConfig: ListenerConfig{
		Tunnel: stubTunnel{},
		Type:   C.SOCKS5,
	}}
	src := M.SocksaddrFrom(netip.MustParseAddr("192.0.2.1"), 12345)
	dst := M.SocksaddrFrom(netip.MustParseAddr("198.51.100.1"), 443)
	inAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}
	pkt := &packet{lAddr: inAddr}
	h.handlePacket(context.Background(), pkt, src, dst)
	defer func() {
		if pkt.metadata != nil {
			releasePacketMetadata(pkt.metadata)
		}
	}()

	if pkt.metadata.SrcIP.String() != "192.0.2.1" || pkt.metadata.SrcPort != 12345 {
		t.Fatalf("src = %s:%d", pkt.metadata.SrcIP, pkt.metadata.SrcPort)
	}
	if pkt.metadata.DstIP.String() != "198.51.100.1" || pkt.metadata.DstPort != 443 {
		t.Fatalf("dst = %s:%d", pkt.metadata.DstIP, pkt.metadata.DstPort)
	}
	if pkt.metadata.InIP.String() != "127.0.0.1" || pkt.metadata.InPort != 1080 {
		t.Fatalf("in = %s:%d", pkt.metadata.InIP, pkt.metadata.InPort)
	}
	rawSrc, ok := pkt.metadata.RawSrcAddr.(*net.UDPAddr)
	if !ok || rawSrc.Port != 12345 {
		t.Fatalf("raw src = %#v", pkt.metadata.RawSrcAddr)
	}
}

func BenchmarkHandlePacket(b *testing.B) {
	h := &ListenerHandler{ListenerConfig: ListenerConfig{
		Tunnel: stubTunnel{},
		Type:   C.SOCKS5,
	}}
	src := M.SocksaddrFrom(netip.MustParseAddr("192.0.2.1"), 12345)
	dst := M.SocksaddrFrom(netip.MustParseAddr("198.51.100.1"), 443)
	inAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkt := &packet{lAddr: inAddr}
		h.handlePacket(ctx, pkt, src, dst)
		pkt.Drop()
	}
}

func TestHandlePacketKeepsWriteBackAfterDrop(t *testing.T) {
	var mu sync.Mutex
	var writer network.NetPacketWriter = dropWriteBackWriter{}
	pkt := &packet{writer: &writer, mutex: &mu}
	pkt.Drop()
	if pkt.writer == nil || pkt.mutex == nil {
		t.Fatal("Drop recycled NAT WriteBack handle")
	}
	if _, err := pkt.WriteBack([]byte("x"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}); err != nil {
		t.Fatal(err)
	}
}

func TestApplySocksDestinationCopiesNonDomainFqdn(t *testing.T) {
	metadata := &C.Metadata{}
	applySocksDestination(metadata, M.Socksaddr{Fqdn: "_dns.resolver.arpa", Port: 53})
	if metadata.Host != "_dns.resolver.arpa" || metadata.DstPort != 53 {
		t.Fatalf("fqdn dest = host=%q port=%d", metadata.Host, metadata.DstPort)
	}
}

type dropWriteBackWriter struct{}

func (dropWriteBackWriter) WriteTo(p []byte, _ net.Addr) (int, error) {
	return len(p), nil
}

func (dropWriteBackWriter) WritePacket(*buf.Buffer, M.Socksaddr) error {
	return nil
}
