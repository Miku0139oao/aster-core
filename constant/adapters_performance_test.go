package constant

import (
	"net"
	"net/netip"
	"testing"
)

type benchmarkUDPPacket struct {
	addr *net.UDPAddr
}

func (*benchmarkUDPPacket) Data() []byte                            { return nil }
func (*benchmarkUDPPacket) WriteBack([]byte, net.Addr) (int, error) { return 0, nil }
func (*benchmarkUDPPacket) Drop()                                   {}
func (p *benchmarkUDPPacket) LocalAddr() net.Addr                   { return p.addr }

type namespacedUDPPacket struct {
	benchmarkUDPPacket
	in net.Addr
}

func (p *namespacedUDPPacket) InAddr() net.Addr { return p.in }

type packetAdapterLifecycleProbe struct {
	benchmarkUDPPacket
	drops  int
	writes int
}

func (p *packetAdapterLifecycleProbe) WriteBack([]byte, net.Addr) (int, error) {
	p.writes++
	return 1, nil
}

func (p *packetAdapterLifecycleProbe) Drop() {
	p.drops++
}

func TestUDPNatKeyRoundTrip(t *testing.T) {
	addrPort := netip.MustParseAddrPort("[2001:db8::1]:12345")
	key := NewUDPNatKey(net.UDPAddrFromAddrPort(addrPort))
	if key.AddrPort != addrPort || key.Raw != "" {
		t.Fatalf("unexpected address key: %#v", key)
	}
	if parsed := ParseUDPNatKey(key.String()); parsed != key {
		t.Fatalf("key did not round trip: %#v != %#v", parsed, key)
	}

	raw := "custom:flow-key"
	if parsed := ParseUDPNatKey(raw); parsed.Raw != raw || parsed.AddrPort.IsValid() {
		t.Fatalf("unexpected raw key: %#v", parsed)
	}
}

func TestUDPNatKeyIncludesIngressNamespace(t *testing.T) {
	source := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.10:12345"))
	packetA := &namespacedUDPPacket{
		benchmarkUDPPacket: benchmarkUDPPacket{addr: source},
		in:                 net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1080")),
	}
	packetB := &namespacedUDPPacket{
		benchmarkUDPPacket: benchmarkUDPPacket{addr: source},
		in:                 net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1081")),
	}
	keyA := NewUDPNatKeyForPacket(packetA, &Metadata{Type: SOCKS5, InName: "socks-a"})
	keyB := NewUDPNatKeyForPacket(packetB, &Metadata{Type: SOCKS5, InName: "socks-b"})
	if keyA == keyB {
		t.Fatal("equal client tuples from different inbounds collided")
	}
	if keyA.AddrPort != keyB.AddrPort || keyA.IngressAddrPort == keyB.IngressAddrPort {
		t.Fatalf("unexpected namespaced keys: %#v %#v", keyA, keyB)
	}
	if repeated := NewUDPNatKeyForPacket(packetA, &Metadata{Type: SOCKS5, InName: "socks-a"}); repeated != keyA {
		t.Fatalf("stable ingress produced a different key: %#v != %#v", repeated, keyA)
	}
}

func TestPacketAdapterWriteBackOutlivesWrapper(t *testing.T) {
	packet := &packetAdapterLifecycleProbe{benchmarkUDPPacket: benchmarkUDPPacket{
		addr: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:12345")),
	}}
	adapter := NewPacketAdapter(packet, &Metadata{})
	writeBack := adapter.WriteBackTarget()
	adapter.Drop()

	if packet.drops != 1 {
		t.Fatalf("unexpected drop count: %d", packet.drops)
	}
	if _, err := writeBack.WriteBack(nil, packet.addr); err != nil {
		t.Fatal(err)
	}
	if packet.writes != 1 {
		t.Fatalf("unexpected write count: %d", packet.writes)
	}
}

func TestNestedPacketAdapterWriteBackOutlivesAllWrappers(t *testing.T) {
	packet := &packetAdapterLifecycleProbe{benchmarkUDPPacket: benchmarkUDPPacket{
		addr: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.2:23456")),
	}}
	inner := NewPacketAdapter(packet, &Metadata{})
	outer := NewPacketAdapter(inner, &Metadata{})
	writeBack := outer.WriteBackTarget()
	outer.Drop()

	if packet.drops != 1 {
		t.Fatalf("unexpected nested drop count: %d", packet.drops)
	}
	if _, err := writeBack.WriteBack(nil, packet.addr); err != nil {
		t.Fatal(err)
	}
	if packet.writes != 1 {
		t.Fatalf("unexpected nested write count: %d", packet.writes)
	}
}

func BenchmarkNewPacketAdapter(b *testing.B) {
	packet := &benchmarkUDPPacket{addr: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:12345"))}
	metadata := &Metadata{NetWork: UDP, DstIP: netip.MustParseAddr("198.51.100.1"), DstPort: 443}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		adapter := NewPacketAdapter(packet, metadata)
		if !adapter.Key().IsValid() {
			b.Fatal("empty packet key")
		}
		adapter.Drop()
	}
}
