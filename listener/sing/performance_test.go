package sing

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
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

type captureUDPTunnel struct {
	mu      sync.Mutex
	packets []C.UDPPacket
}

func (t *captureUDPTunnel) HandleTCPConn(net.Conn, *C.Metadata) {}
func (t *captureUDPTunnel) HandleUDPPacket(packet C.UDPPacket, _ *C.Metadata) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.packets = append(t.packets, packet)
}
func (t *captureUDPTunnel) NatTable() C.NatTable { return nil }

func (t *captureUDPTunnel) last() *packet {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.packets) == 0 {
		return nil
	}
	pkt, _ := t.packets[len(t.packets)-1].(*packet)
	return pkt
}

type countingWriter struct {
	n atomic.Int32
}

func (w *countingWriter) WriteTo(p []byte, _ net.Addr) (int, error) {
	w.n.Add(1)
	return len(p), nil
}

func (w *countingWriter) WritePacket(*buf.Buffer, M.Socksaddr) error {
	return nil
}

func newPacketForTest(t *testing.T, tunnel C.Tunnel, writer network.PacketWriter) *packet {
	t.Helper()
	h := &ListenerHandler{ListenerConfig: ListenerConfig{
		Tunnel: tunnel,
		Type:   C.TUN,
	}}
	src := M.SocksaddrFrom(netip.MustParseAddr("192.0.2.1"), 12345)
	dst := M.SocksaddrFrom(netip.MustParseAddr("198.51.100.1"), 443)
	buff := buf.NewPacket()
	_, _ = buff.Write([]byte("x"))
	h.NewPacket(context.Background(), netip.MustParseAddrPort("192.0.2.1:12345"), buff, M.Metadata{
		Source:      src,
		Destination: dst,
	}, func(network.PacketConn) network.PacketWriter {
		return writer
	})
	captured, ok := tunnel.(*captureUDPTunnel)
	if !ok {
		t.Fatal("test tunnel is not captureUDPTunnel")
	}
	pkt := captured.last()
	if pkt == nil {
		t.Fatal("NewPacket did not emit a packet")
	}
	return pkt
}

func TestNewPacketIndependentWriteHandles(t *testing.T) {
	tunnel := &captureUDPTunnel{}
	w1 := &countingWriter{}
	w2 := &countingWriter{}
	p1 := newPacketForTest(t, tunnel, w1)
	p2 := newPacketForTest(t, tunnel, w2)
	if p1.mutex == nil || p1.writer == nil || p2.mutex == nil || p2.writer == nil {
		t.Fatal("missing write handle pointers")
	}
	if p1.mutex == p2.mutex || p1.writer == p2.writer {
		t.Fatal("NewPacket reused write handle across packets")
	}
	if _, err := p1.WriteBack([]byte("a"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}); err != nil {
		t.Fatal(err)
	}
	if w1.n.Load() != 1 || w2.n.Load() != 0 {
		t.Fatalf("independent write counts w1=%d w2=%d", w1.n.Load(), w2.n.Load())
	}
	p1.Drop()
	p2.Drop()
}

func TestNewPacketWriteBackAfterDrop(t *testing.T) {
	tunnel := &captureUDPTunnel{}
	w := &countingWriter{}
	pkt := newPacketForTest(t, tunnel, w)
	pkt.Drop()
	if pkt.writer == nil || pkt.mutex == nil {
		t.Fatal("Drop recycled NewPacket WriteBack handle")
	}
	if _, err := pkt.WriteBack([]byte("x"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}); err != nil {
		t.Fatal(err)
	}
	if w.n.Load() != 1 {
		t.Fatalf("writes = %d", w.n.Load())
	}
}

func TestNewPacketConcurrentWriteBackAndDrop(t *testing.T) {
	tunnel := &captureUDPTunnel{}
	w := &countingWriter{}
	pkt := newPacketForTest(t, tunnel, w)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 64; i++ {
			_, _ = pkt.WriteBack([]byte("x"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53})
		}
	}()
	pkt.Drop()
	<-done
	if pkt.writer == nil || pkt.mutex == nil {
		t.Fatal("Drop recycled NewPacket WriteBack handle")
	}
}

func BenchmarkNewPacket(b *testing.B) {
	h := &ListenerHandler{ListenerConfig: ListenerConfig{
		Tunnel: dropUDPTunnel{},
		Type:   C.TUN,
	}}
	src := M.SocksaddrFrom(netip.MustParseAddr("192.0.2.1"), 12345)
	dst := M.SocksaddrFrom(netip.MustParseAddr("198.51.100.1"), 443)
	meta := M.Metadata{Source: src, Destination: dst}
	key := netip.MustParseAddrPort("192.0.2.1:12345")
	writer := dropWriteBackWriter{}
	init := func(network.PacketConn) network.PacketWriter { return writer }
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buff := buf.NewPacket()
		_, _ = buff.Write([]byte("x"))
		h.NewPacket(ctx, key, buff, meta, init)
	}
}

type dropUDPTunnel struct{}

func (dropUDPTunnel) HandleTCPConn(net.Conn, *C.Metadata) {}
func (dropUDPTunnel) HandleUDPPacket(packet C.UDPPacket, _ *C.Metadata) {
	if packet != nil {
		packet.Drop()
	}
}
func (dropUDPTunnel) NatTable() C.NatTable { return nil }
