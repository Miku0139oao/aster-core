package socks

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/transport/socks5"
)

type udpCaptureTunnel struct {
	mu       sync.Mutex
	packet   C.UDPPacket
	metadata *C.Metadata
	calls    int
}

func (t *udpCaptureTunnel) HandleTCPConn(net.Conn, *C.Metadata) {}
func (t *udpCaptureTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.packet = packet
	t.metadata = metadata
	t.calls++
}
func (t *udpCaptureTunnel) NatTable() C.NatTable { return nil }

type stubPacketConn struct {
	local net.Addr
}

func (c stubPacketConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, io.EOF }
func (c stubPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return len(b), nil
}
func (c stubPacketConn) Close() error                     { return nil }
func (c stubPacketConn) LocalAddr() net.Addr              { return c.local }
func (c stubPacketConn) SetDeadline(time.Time) error      { return nil }
func (c stubPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c stubPacketConn) SetWriteDeadline(time.Time) error { return nil }

type stringAddr struct{ net, addr string }

func (a stringAddr) Network() string { return a.net }
func (a stringAddr) String() string  { return a.addr }

func socksUDPPayload(t *testing.T, target socks5.Addr) []byte {
	t.Helper()
	buf, err := socks5.EncodeUDPPacket(target, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func ipv6MappedSocksAddr(ip net.IP, port uint16) socks5.Addr {
	addr := make(socks5.Addr, 1+net.IPv6len+2)
	addr[0] = socks5.AtypIPv6
	copy(addr[1:], ip.To16())
	binary.BigEndian.PutUint16(addr[1+net.IPv6len:], port)
	return addr
}

func TestSocksUDPMetadataMatchesNewPacket(t *testing.T) {
	overrideDst := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 9999}
	cases := []struct {
		name      string
		target    socks5.Addr
		src       net.Addr
		local     net.Addr
		additions []inbound.Addition
	}{
		{
			name:   "ipv4",
			target: socks5.ParseAddr("1.2.3.4:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "ipv6",
			target: socks5.ParseAddr("[2001:db8::1]:443"),
			src:    &net.UDPAddr{IP: net.ParseIP("2001:db8::2"), Port: 23456},
			local:  &net.UDPAddr{IP: net.ParseIP("2001:db8::3"), Port: 1080},
		},
		{
			name:   "ipv6-mapped-dest",
			target: ipv6MappedSocksAddr(net.ParseIP("192.0.2.10"), 53),
			src:    &net.UDPAddr{IP: net.IPv4(198, 51, 100, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "ipv6-zone-src-in",
			target: socks5.ParseAddr("[2001:db8::9]:853"),
			src:    &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 12345, Zone: "eth0"},
			local:  &net.UDPAddr{IP: net.ParseIP("fe80::2"), Port: 1080, Zone: "eth1"},
		},
		{
			name:   "mapped-src-ipv4-slice",
			target: socks5.ParseAddr("8.8.8.8:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1080},
		},
		{
			name:   "domain",
			target: socks5.ParseAddr("example.com:443"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "trailing-dot-domain",
			target: socks5.ParseAddr("example.com.:443"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "unusual-domain",
			target: socks5.ParseAddr("_dns.resolver.arpa:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "dest-port0",
			target: socks5.ParseAddr("1.2.3.4:0"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "src-port0",
			target: socks5.ParseAddr("1.2.3.4:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 0},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "tcp-src-and-in",
			target: socks5.ParseAddr("1.2.3.4:80"),
			src:    &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "string-src",
			target: socks5.ParseAddr("1.2.3.4:53"),
			src:    stringAddr{net: "udp", addr: "192.0.2.7:12345"},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
		},
		{
			name:   "nil-inaddr",
			target: socks5.ParseAddr("1.2.3.4:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  nil,
		},
		{
			name:   "additions-override",
			target: socks5.ParseAddr("1.2.3.4:53"),
			src:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
			local:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080},
			additions: []inbound.Addition{
				inbound.WithInName("socks-in"),
				inbound.WithInUser("alice"),
				inbound.WithSpecialRules("rule"),
				inbound.WithSpecialProxy("proxy"),
				inbound.WithDSCP(46),
				inbound.WithDstAddr(overrideDst),
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.target == nil {
				t.Fatal("nil target")
			}
			pc := stubPacketConn{local: tc.local}
			legacy := &packet{pc: pc, rAddr: tc.src}
			_, want := inbound.NewPacket(tc.target, legacy, C.SOCKS5, tc.additions...)

			gotPkt := &packet{pc: pc, rAddr: tc.src}
			got := fillSocksUDPMetadata(gotPkt, tc.target, tc.additions...)
			defer gotPkt.Drop()

			if gotPkt.metadata != got {
				t.Fatal("metadata not stored on packet")
			}
			if raw, ok := got.RawDstAddr.(*net.UDPAddr); ok && raw != nil && raw != &gotPkt.rawDst {
				t.Fatal("RawDstAddr is not packet-owned")
			}
			assertMetadataMatch(t, got, want)
		})
	}
}

func TestHandleSocksUDPMatchesNewPacket(t *testing.T) {
	target := socks5.ParseAddr("1.2.3.4:53")
	src := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	pc := stubPacketConn{local: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}}
	additions := []inbound.Addition{inbound.WithInName("socks-in")}

	legacy := &packet{pc: pc, rAddr: src}
	_, want := inbound.NewPacket(target, legacy, C.SOCKS5, additions...)

	tunnel := &udpCaptureTunnel{}
	var putCalls atomic.Int32
	handleSocksUDP(pc, tunnel, socksUDPPayload(t, target), func() { putCalls.Add(1) }, src, additions...)
	if tunnel.calls != 1 {
		t.Fatalf("HandleUDPPacket calls = %d", tunnel.calls)
	}
	assertMetadataMatch(t, tunnel.metadata, want)
	if tunnel.packet == nil {
		t.Fatal("missing packet")
	}
	tunnel.packet.Drop()
	if putCalls.Load() != 1 {
		t.Fatalf("put calls = %d", putCalls.Load())
	}
}

func TestHandleSocksUDPDecodeErrorsKeepOldBehavior(t *testing.T) {
	cases := map[string][]byte{
		"short":    {0, 0, 0},
		"reserved": {1, 0, 0, 1, 1, 2, 3, 4, 0, 53},
		"fragment": {0, 0, 1, 1, 1, 2, 3, 4, 0, 53},
		"bad-atyp": {0, 0, 0, 99, 1, 2, 3, 4, 0, 53},
	}
	for name, buf := range cases {
		t.Run(name, func(t *testing.T) {
			tunnel := &udpCaptureTunnel{}
			var putCalls atomic.Int32
			handleSocksUDP(stubPacketConn{}, tunnel, buf, func() { putCalls.Add(1) }, &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 1})
			if tunnel.calls != 0 {
				t.Fatalf("unexpected HandleUDPPacket calls = %d", tunnel.calls)
			}
			if putCalls.Load() != 1 {
				t.Fatalf("put calls = %d", putCalls.Load())
			}
		})
	}
}

func TestSocksUDPMetadataPoolClearsState(t *testing.T) {
	pc := stubPacketConn{local: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}}
	src := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
	pkt := &packet{pc: pc, rAddr: src, payload: []byte("x"), put: func() {}}
	fillSocksUDPMetadata(pkt, socks5.ParseAddr("example.com:443"), inbound.WithInUser("alice"))
	if pkt.metadata.Host != "example.com" || pkt.metadata.InUser != "alice" {
		t.Fatalf("setup metadata = %#v", pkt.metadata)
	}
	raw := pkt.metadata.RawSrcAddr
	pkt.Drop()
	if pkt.metadata != nil {
		t.Fatal("metadata pointer retained after Drop")
	}
	if pkt.pc == nil || pkt.rAddr == nil {
		t.Fatal("Drop recycled WriteBack handle")
	}

	pkt2 := &packet{pc: pc, rAddr: src}
	got := fillSocksUDPMetadata(pkt2, socks5.ParseAddr("1.2.3.4:53"))
	defer pkt2.Drop()
	if got.Host != "" || got.InUser != "" || got.SpecialRules != "" || got.SpecialProxy != "" {
		t.Fatalf("pooled metadata retained strings: %#v", got)
	}
	if got.NetWork != C.UDP || got.Type != C.SOCKS5 {
		t.Fatalf("base fields = net=%s type=%s", got.NetWork, got.Type)
	}
	if got.RawSrcAddr == raw && raw != src {
		t.Fatal("pooled metadata retained previous RawSrcAddr")
	}
	if !got.DstIP.Is4() {
		t.Fatalf("expected ipv4 dest, got %s", got.DstIP)
	}
}

func TestSocksUDPWriteBackAndDropOnce(t *testing.T) {
	var putCalls atomic.Int32
	pkt := &packet{
		pc:      stubPacketConn{local: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1080}},
		rAddr:   &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345},
		payload: []byte("payload"),
		put:     func() { putCalls.Add(1) },
	}
	fillSocksUDPMetadata(pkt, socks5.ParseAddr("1.2.3.4:53"), inbound.WithInUser("bob"))
	pkt.Drop()
	if putCalls.Load() != 1 {
		t.Fatalf("put calls = %d", putCalls.Load())
	}
	pkt.Drop()
	if putCalls.Load() != 1 {
		t.Fatalf("Drop recycled payload more than once: %d", putCalls.Load())
	}
	if _, err := pkt.WriteBack([]byte("x"), &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}); err != nil {
		t.Fatal(err)
	}
}

func TestSocksUDPDropDoesNotPoolPacket(t *testing.T) {
	a := &packet{pc: stubPacketConn{}, rAddr: &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 1}}
	b := &packet{pc: stubPacketConn{}, rAddr: &net.UDPAddr{IP: net.IPv4(2, 2, 2, 2), Port: 2}}
	fillSocksUDPMetadata(a, socks5.ParseAddr("1.2.3.4:53"))
	fillSocksUDPMetadata(b, socks5.ParseAddr("4.3.2.1:53"))
	a.Drop()
	b.Drop()
	if a == b {
		t.Fatal("packet objects were pooled")
	}
}

func assertMetadataMatch(t *testing.T, got, want *C.Metadata) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("nil metadata got=%v want=%v", got, want)
	}
	if got.NetWork != want.NetWork || got.Type != want.Type {
		t.Fatalf("net/type got=%s/%s want=%s/%s", got.NetWork, got.Type, want.NetWork, want.Type)
	}
	if got.Host != want.Host || got.DstIP != want.DstIP || got.DstPort != want.DstPort {
		t.Fatalf("dst got host=%q ip=%s port=%d want host=%q ip=%s port=%d",
			got.Host, got.DstIP, got.DstPort, want.Host, want.DstIP, want.DstPort)
	}
	if got.SrcIP != want.SrcIP || got.SrcPort != want.SrcPort {
		t.Fatalf("src got %s:%d want %s:%d", got.SrcIP, got.SrcPort, want.SrcIP, want.SrcPort)
	}
	if got.InIP != want.InIP || got.InPort != want.InPort {
		t.Fatalf("in got %s:%d want %s:%d", got.InIP, got.InPort, want.InIP, want.InPort)
	}
	if got.InName != want.InName || got.InUser != want.InUser || got.SpecialRules != want.SpecialRules || got.SpecialProxy != want.SpecialProxy || got.DSCP != want.DSCP {
		t.Fatalf("additions got name=%q user=%q rules=%q proxy=%q dscp=%d want name=%q user=%q rules=%q proxy=%q dscp=%d",
			got.InName, got.InUser, got.SpecialRules, got.SpecialProxy, got.DSCP,
			want.InName, want.InUser, want.SpecialRules, want.SpecialProxy, want.DSCP)
	}
	if !sameAddr(got.RawSrcAddr, want.RawSrcAddr) {
		t.Fatalf("RawSrcAddr got=%v want=%v", got.RawSrcAddr, want.RawSrcAddr)
	}
	if !sameUDPAddrValue(got.RawDstAddr, want.RawDstAddr) {
		t.Fatalf("RawDstAddr got=%v want=%v", got.RawDstAddr, want.RawDstAddr)
	}
}

func sameAddr(a, b net.Addr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Network() == b.Network() && a.String() == b.String()
}

func addrIsNil(a net.Addr) bool {
	if a == nil {
		return true
	}
	switch v := a.(type) {
	case *net.UDPAddr:
		return v == nil
	case *net.TCPAddr:
		return v == nil
	default:
		return false
	}
}

func sameUDPAddrValue(a, b net.Addr) bool {
	if addrIsNil(a) || addrIsNil(b) {
		return addrIsNil(a) && addrIsNil(b)
	}
	ua, oka := a.(*net.UDPAddr)
	ub, okb := b.(*net.UDPAddr)
	if !oka || !okb {
		return sameAddr(a, b)
	}
	if ua.Port != ub.Port || ua.Zone != ub.Zone {
		return false
	}
	if ua.IP == nil || ub.IP == nil {
		return ua.IP == nil && ub.IP == nil
	}
	return ua.IP.Equal(ub.IP)
}
