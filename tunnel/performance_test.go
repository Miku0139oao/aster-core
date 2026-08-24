package tunnel

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	N "github.com/Miku0139oao/aster-core/common/net"
	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/component/nat"
	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
	R "github.com/Miku0139oao/aster-core/rules"
	"github.com/Miku0139oao/aster-core/tunnel/statistic"
)

type deadlinePacketConn struct {
	C.PacketConn
	calls    atomic.Int64
	deadline atomic.Int64
}

type relayBenchmarkConn struct {
	N.ExtendedConn
}

func (*relayBenchmarkConn) Chains() C.Chain               { return nil }
func (*relayBenchmarkConn) ProviderChains() C.Chain       { return nil }
func (*relayBenchmarkConn) AppendToChains(C.ProxyAdapter) {}
func (*relayBenchmarkConn) RemoteDestination() string     { return "benchmark" }
func (c *relayBenchmarkConn) UpstreamReader() any         { return c.ExtendedConn }
func (c *relayBenchmarkConn) UpstreamWriter() any         { return c.ExtendedConn }
func (*relayBenchmarkConn) ReaderReplaceable() bool       { return true }
func (*relayBenchmarkConn) WriterReplaceable() bool       { return true }

func (c *deadlinePacketConn) SetReadDeadline(deadline time.Time) error {
	c.calls.Add(1)
	c.deadline.Store(deadline.UnixNano())
	return nil
}

func (c *deadlinePacketConn) WriteTo(payload []byte, _ net.Addr) (int, error) {
	return len(payload), nil
}

type relayBenchmarkPacket struct {
	data     []byte
	local    net.Addr
	released chan struct{}
}

func (p *relayBenchmarkPacket) Data() []byte { return p.data }
func (p *relayBenchmarkPacket) WriteBack(payload []byte, _ net.Addr) (int, error) {
	return len(payload), nil
}
func (p *relayBenchmarkPacket) Drop()               { p.released <- struct{}{} }
func (p *relayBenchmarkPacket) LocalAddr() net.Addr { return p.local }

type benchmarkAdapter struct{}

func (*benchmarkAdapter) Name() string                 { return "DIRECT" }
func (*benchmarkAdapter) Type() C.AdapterType          { return C.Direct }
func (*benchmarkAdapter) Addr() string                 { return "" }
func (*benchmarkAdapter) SupportUDP() bool             { return true }
func (*benchmarkAdapter) ProxyInfo() C.ProxyInfo       { return C.ProxyInfo{} }
func (*benchmarkAdapter) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (*benchmarkAdapter) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, C.ErrNotSupport
}

func (*benchmarkAdapter) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (*benchmarkAdapter) SupportUOT() bool                 { return false }
func (*benchmarkAdapter) IsL3Protocol(*C.Metadata) bool    { return false }
func (*benchmarkAdapter) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (*benchmarkAdapter) Close() error                     { return nil }
func (a *benchmarkAdapter) Adapter() C.ProxyAdapter        { return a }
func (*benchmarkAdapter) AliveForTestUrl(string) bool      { return true }
func (*benchmarkAdapter) DelayHistory() []C.DelayHistory   { return nil }
func (*benchmarkAdapter) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (*benchmarkAdapter) LastDelayForTestUrl(string) uint16 { return 0 }
func (*benchmarkAdapter) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

type lifecycleTestProvider struct {
	name   string
	closed atomic.Int64
}

func (p *lifecycleTestProvider) Name() string             { return p.name }
func (*lifecycleTestProvider) VehicleType() P.VehicleType { return P.Compatible }
func (*lifecycleTestProvider) Type() P.ProviderType       { return P.Rule }
func (*lifecycleTestProvider) Initial() error             { return nil }
func (*lifecycleTestProvider) Update() error              { return nil }
func (p *lifecycleTestProvider) Close() error {
	p.closed.Add(1)
	return nil
}

func TestCloseRetiredProvidersUsesObjectIdentity(t *testing.T) {
	retired := &lifecycleTestProvider{name: "same-name"}
	active := &lifecycleTestProvider{name: "same-name"}
	retainedUnderNewName := &lifecycleTestProvider{name: "old-name"}
	closeRetiredProviders(
		map[string]*lifecycleTestProvider{"old": retired, "renamed": retainedUnderNewName},
		map[string]*lifecycleTestProvider{"replacement": active, "new-name": retainedUnderNewName},
	)
	if retired.closed.Load() != 1 {
		t.Fatalf("replaced provider close count = %d", retired.closed.Load())
	}
	if active.closed.Load() != 0 || retainedUnderNewName.closed.Load() != 0 {
		t.Fatal("active provider was closed")
	}
}

func TestPacketSenderDestinationMapping(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	t.Cleanup(sender.Close)

	originIP := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.1")}
	targetIP := &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1")}
	sender.AddMapping(originIP, targetIP)
	if got := sender.targetAddress(originIP); got == nil || got.AddrPort() != targetIP.AddrPort() {
		t.Fatalf("unexpected IP target: %v", got)
	}
	if got := sender.RestoreReadFrom(targetIP.AddrPort()); got != originIP.AddrPort() {
		t.Fatalf("unexpected restored endpoint: %s", got)
	}

	originHost := &C.Metadata{Host: "example.com", DstIP: originIP.DstIP}
	targetHost := &C.Metadata{DstIP: netip.MustParseAddr("203.0.113.1")}
	sender.AddMapping(originHost, targetHost)
	if got := sender.targetAddress(originHost); got == nil || got.AddrPort() != targetHost.AddrPort() {
		t.Fatalf("unexpected host target: %v", got)
	}
}

func TestPacketSenderDestinationMappingUnmaps(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	t.Cleanup(sender.Close)

	origin4in6 := netip.MustParseAddr("::ffff:192.0.2.1")
	target4in6 := netip.MustParseAddr("::ffff:198.51.100.1")
	origin := netip.MustParseAddr("192.0.2.1")
	target := netip.MustParseAddr("198.51.100.1")

	const port = 53
	sender.AddMapping(&C.Metadata{DstIP: origin4in6, DstPort: port}, &C.Metadata{DstIP: target4in6, DstPort: port})
	originAddrPort := netip.AddrPortFrom(origin, port)
	targetAddrPort := netip.AddrPortFrom(target, port)
	if got := sender.RestoreReadFrom(targetAddrPort); got != originAddrPort {
		t.Fatalf("unmapped lookup = %s, want %s", got, originAddrPort)
	}
	if got := sender.RestoreReadFrom(netip.AddrPortFrom(target4in6, port)); got != originAddrPort {
		t.Fatalf("4in6 lookup = %s, want %s", got, originAddrPort)
	}
	if got := sender.targetAddress(&C.Metadata{DstIP: origin4in6, DstPort: port}); got == nil || got.AddrPort().Addr() != target {
		t.Fatalf("4in6 origin key should resolve to unmapped target, got %v", got)
	}
	if got := sender.targetAddress(&C.Metadata{DstIP: origin, DstPort: port}); got == nil || got.AddrPort().Addr() != target {
		t.Fatalf("unmapped origin key should hit the same mapping, got %v", got)
	}
}

func TestPacketSenderReverseMappingDistinguishesPorts(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	t.Cleanup(sender.Close)
	targetIP := netip.MustParseAddr("198.51.100.10")
	firstOrigin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.10"), DstPort: 53}
	secondOrigin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.11"), DstPort: 443}
	firstTarget := &C.Metadata{DstIP: targetIP, DstPort: 53}
	secondTarget := &C.Metadata{DstIP: targetIP, DstPort: 443}
	sender.AddMapping(firstOrigin, firstTarget)
	sender.AddMapping(secondOrigin, secondTarget)

	if got := sender.RestoreReadFrom(firstTarget.AddrPort()); got != firstOrigin.AddrPort() {
		t.Fatalf("first endpoint restored as %s", got)
	}
	if got := sender.RestoreReadFrom(secondTarget.AddrPort()); got != secondOrigin.AddrPort() {
		t.Fatalf("second endpoint restored as %s", got)
	}
}

func TestPacketSenderDestinationMappingIsBounded(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	t.Cleanup(sender.Close)
	for i := 0; i < maxUDPDestinationMappings; i++ {
		origin := &C.Metadata{DstIP: netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), DstPort: uint16(i)}
		target := &C.Metadata{DstIP: netip.AddrFrom4([4]byte{11, byte(i >> 16), byte(i >> 8), byte(i)}), DstPort: uint16(i)}
		if !sender.addMapping(origin, target) {
			t.Fatalf("mapping %d rejected before limit", i)
		}
	}
	overflowOrigin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.1"), DstPort: 1}
	overflowTarget := &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1"), DstPort: 1}
	if sender.addMapping(overflowOrigin, overflowTarget) {
		t.Fatal("mapping beyond per-association limit was accepted")
	}
	if got := sender.targetAddress(overflowOrigin); got != nil {
		t.Fatalf("rejected mapping retained target %v", got)
	}
}

func TestPacketSenderCoalescesReadDeadlines(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	t.Cleanup(sender.Close)
	conn := &deadlinePacketConn{}
	before := time.Now()

	sender.RefreshReadDeadline(conn)
	sender.RefreshReadDeadline(conn)
	if calls := conn.calls.Load(); calls != 1 {
		t.Fatalf("SetReadDeadline calls = %d, want 1", calls)
	}
	deadline := time.Unix(0, conn.deadline.Load())
	if deadline.Before(before.Add(udpTimeout)) {
		t.Fatalf("deadline %s is earlier than the idle timeout", deadline)
	}

	sender.nextDeadlineRefresh.Store(0)
	sender.RefreshReadDeadline(conn)
	if calls := conn.calls.Load(); calls != 2 {
		t.Fatalf("SetReadDeadline calls after refresh = %d, want 2", calls)
	}
}

func TestMatchUsesRoutingSnapshot(t *testing.T) {
	oldMode := Mode()
	oldProxies, oldProviders := proxies, providers
	oldRules, oldSubRules, oldRuleProviders := rules, subRules, ruleProviders
	t.Cleanup(func() {
		SetMode(oldMode)
		UpdateProxies(oldProxies, oldProviders)
		UpdateRules(oldRules, oldSubRules, oldRuleProviders)
	})

	direct := &benchmarkAdapter{}
	rule, err := R.ParseRule("MATCH", "", direct.Name(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	UpdateProxies(map[string]C.Proxy{direct.Name(): direct}, nil)
	UpdateRules([]C.Rule{rule}, nil, nil)
	SetMode(Rule)

	configMux.Lock()
	done := make(chan error, 1)
	go func() {
		_, _, err := match(&C.Metadata{NetWork: C.TCP, Host: "example.com", DstPort: 443}, C.RuleMatchHelper{})
		done <- err
	}()
	select {
	case err := <-done:
		configMux.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		configMux.Unlock()
		t.Fatal("rule match blocked on the configuration write lock")
	}
}

func BenchmarkPacketSenderMappingInsert(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sender := newPacketSender().(*packetSender)
				for j := 0; j < count; j++ {
					origin := &C.Metadata{DstIP: netip.AddrFrom4([4]byte{10, byte(j >> 16), byte(j >> 8), byte(j)}), DstPort: uint16(j)}
					target := &C.Metadata{DstIP: netip.AddrFrom4([4]byte{11, byte(j >> 16), byte(j >> 8), byte(j)}), DstPort: uint16(j)}
					if !sender.addMapping(origin, target) {
						b.Fatal("mapping limit reached")
					}
				}
				sender.Close()
			}
		})
	}
}

func BenchmarkPacketSenderDestinationLookup(b *testing.B) {
	sender := newPacketSender().(*packetSender)
	b.Cleanup(sender.Close)
	origin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.1")}
	target := &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1")}
	sender.AddMapping(origin, target)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := sender.targetAddress(origin); got == nil || got.AddrPort() != target.AddrPort() {
			b.Fatalf("unexpected target: %v", got)
		}
	}
}

func BenchmarkPacketSenderReverseLookup(b *testing.B) {
	sender := newPacketSender().(*packetSender)
	b.Cleanup(sender.Close)
	origin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.1")}
	target := &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1")}
	sender.AddMapping(origin, target)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := sender.RestoreReadFrom(target.AddrPort()); got != origin.AddrPort() {
			b.Fatalf("unexpected origin: %s", got)
		}
	}
}

func BenchmarkUDPMetadataPrecheck(b *testing.B) {
	metadata := &C.Metadata{
		NetWork: C.UDP,
		Type:    C.TUN,
		SrcIP:   netip.MustParseAddr("192.0.2.1"),
		DstIP:   netip.MustParseAddr("198.51.100.1"),
		SrcPort: 12345,
		DstPort: 443,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := preCheckMetadata(metadata); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPacketSenderDeadlineRefresh(b *testing.B) {
	sender := newPacketSender().(*packetSender)
	b.Cleanup(sender.Close)
	conn := &deadlinePacketConn{}
	sender.RefreshReadDeadline(conn)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sender.RefreshReadDeadline(conn)
	}
}

func BenchmarkUDPFlowSteadyState(b *testing.B) {
	sender := newPacketSender().(*packetSender)
	conn := &deadlinePacketConn{}
	origin := &C.Metadata{NetWork: C.UDP, Type: C.TUN, DstIP: netip.MustParseAddr("198.51.100.1"), DstPort: 443}
	sender.AddMapping(origin, origin)
	packet := &relayBenchmarkPacket{
		data:     make([]byte, 1200),
		local:    net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:12345")),
		released: make(chan struct{}, 1),
	}
	writeBack := nat.NewWriteBackProxy(packet)
	go sender.Process(conn, writeBack)
	b.Cleanup(sender.Close)

	b.ReportAllocs()
	b.SetBytes(int64(len(packet.data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sender.Send(C.NewPacketAdapter(packet, origin))
		<-packet.released
	}
}

func BenchmarkTCPRelayThroughput(b *testing.B) {
	sourceWriter, sourceRelay := net.Pipe()
	destinationRelay, destinationReader := net.Pipe()
	manager := &statistic.Manager{}
	metadata := &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}
	remote := &relayBenchmarkConn{ExtendedConn: N.NewExtendedConn(destinationRelay)}
	tracker := statistic.NewTCPTracker(remote, manager, metadata, nil, 0, 0, true)
	payload := make([]byte, 32*1024)
	total := int64(b.N) * int64(len(payload))
	copyDone := make(chan error, 1)
	go func() {
		_, err := io.CopyN(io.Discard, destinationReader, total)
		copyDone <- err
	}()
	relayDone := make(chan struct{})
	go func() {
		handleSocket(sourceRelay, tracker)
		close(relayDone)
	}()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sourceWriter.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = sourceWriter.Close()
	if err := <-copyDone; err != nil {
		b.Fatal(err)
	}
	_ = destinationReader.Close()
	<-relayDone
	if uploaded, _ := manager.Total(); uploaded != total {
		b.Fatalf("tracked upload = %d, want %d", uploaded, total)
	}
}

func BenchmarkMatchDefaultRule(b *testing.B) {
	oldMode := Mode()
	oldProxies, oldProviders := proxies, providers
	oldRules, oldSubRules, oldRuleProviders := rules, subRules, ruleProviders
	b.Cleanup(func() {
		SetMode(oldMode)
		UpdateProxies(oldProxies, oldProviders)
		UpdateRules(oldRules, oldSubRules, oldRuleProviders)
	})

	direct := &benchmarkAdapter{}
	rule, err := R.ParseRule("MATCH", "", direct.Name(), nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	UpdateProxies(map[string]C.Proxy{direct.Name(): direct}, nil)
	UpdateRules([]C.Rule{rule}, nil, nil)
	SetMode(Rule)

	metadata := &C.Metadata{NetWork: C.TCP, Type: C.SOCKS5, Host: "example.com", DstPort: 443}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proxy, matchedRule, err := match(metadata, helper)
		if err != nil || proxy != direct || matchedRule != rule {
			b.Fatalf("unexpected match result: proxy=%v rule=%v err=%v", proxy, matchedRule, err)
		}
	}
}
