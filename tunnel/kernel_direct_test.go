package tunnel

import (
	"net"
	"net/netip"
	"testing"

	"github.com/Miku0139oao/aster-core/component/iface"
	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	C "github.com/Miku0139oao/aster-core/constant"
	RC "github.com/Miku0139oao/aster-core/rules/common"
)

type kernelDirectRejectAdapter struct{ benchmarkAdapter }

func (*kernelDirectRejectAdapter) Name() string              { return "REJECT" }
func (*kernelDirectRejectAdapter) Type() C.AdapterType       { return C.Reject }
func (a *kernelDirectRejectAdapter) Adapter() C.ProxyAdapter { return a }

func setupKernelDirectTest(t *testing.T, testRules []C.Rule) {
	t.Helper()
	oldMode := Mode()
	oldProxies, oldProviders := proxies, providers
	oldRules, oldSubRules, oldRuleProviders := rules, subRules, ruleProviders
	t.Cleanup(func() {
		SetMode(oldMode)
		UpdateProxies(oldProxies, oldProviders)
		UpdateRules(oldRules, oldSubRules, oldRuleProviders)
	})

	direct := &benchmarkAdapter{}
	reject := &kernelDirectRejectAdapter{}
	UpdateProxies(map[string]C.Proxy{
		direct.Name(): direct,
		reject.Name(): reject,
	}, nil)
	UpdateRules(testRules, nil, nil)
	SetMode(Rule)
}

func TestClassifyKernelDirectDestinationRules(t *testing.T) {
	setupKernelDirectTest(t, []C.Rule{
		RC.NewDomainSuffix("blocked.cn", "REJECT"),
		RC.NewDomainSuffix("cn", "DIRECT"),
		RC.NewMatch("REJECT"),
	})

	ip := netip.MustParseAddr("203.0.113.10")
	if !Tunnel.ClassifyKernelDirect(C.Metadata{}, "example.cn", ip) {
		t.Fatal("destination-only DIRECT rule should use kernel path")
	}
	if Tunnel.ClassifyKernelDirect(C.Metadata{}, "blocked.cn", ip) {
		t.Fatal("earlier proxy rule must win")
	}
	if Tunnel.ClassifyKernelDirect(C.Metadata{}, "example.com", ip) {
		t.Fatal("proxy default must stay in userspace")
	}
}

func TestClassifyKernelDirectRejectsContextDependentPrecedence(t *testing.T) {
	processRule, err := RC.NewProcess("browser", "REJECT", C.ProcessName)
	if err != nil {
		t.Fatal(err)
	}
	setupKernelDirectTest(t, []C.Rule{
		processRule,
		RC.NewDomainSuffix("cn", "DIRECT"),
		RC.NewMatch("REJECT"),
	})

	if Tunnel.ClassifyKernelDirect(C.Metadata{}, "example.cn", netip.MustParseAddr("203.0.113.11")) {
		t.Fatal("process-dependent proxy precedence cannot be bypassed by an address-only set")
	}
}

func TestClassifyKernelDirectModes(t *testing.T) {
	setupKernelDirectTest(t, []C.Rule{RC.NewMatch("REJECT")})
	ip := netip.MustParseAddr("203.0.113.12")

	SetMode(Direct)
	if !Tunnel.ClassifyKernelDirect(C.Metadata{}, "example.com", ip) {
		t.Fatal("direct mode should bypass")
	}
	SetMode(Global)
	if Tunnel.ClassifyKernelDirect(C.Metadata{}, "example.com", ip) {
		t.Fatal("global mode must not bypass")
	}
}

func TestClassifyKernelDirectUsesInboundContext(t *testing.T) {
	inNameRule, err := RC.NewInName("named-tun", "DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	setupKernelDirectTest(t, []C.Rule{inNameRule, RC.NewMatch("REJECT")})
	ip := netip.MustParseAddr("203.0.113.13")

	if !Tunnel.ClassifyKernelDirect(C.Metadata{InName: "named-tun"}, "example.com", ip) {
		t.Fatal("fixed named TUN metadata was not applied")
	}
	if Tunnel.ClassifyKernelDirect(C.Metadata{InName: "other-tun"}, "example.com", ip) {
		t.Fatal("another inbound must not reuse the named TUN decision")
	}
	if Tunnel.ClassifyKernelDirect(C.Metadata{SpecialProxy: "REJECT"}, "example.com", ip) {
		t.Fatal("special proxy must override normal rules")
	}
	if !Tunnel.ClassifyKernelDirect(C.Metadata{SpecialProxy: "DIRECT"}, "example.com", ip) {
		t.Fatal("explicit DIRECT special proxy should bypass")
	}
}

func TestClassifyKernelDirectUsesSpecialRules(t *testing.T) {
	setupKernelDirectTest(t, []C.Rule{RC.NewMatch("REJECT")})
	UpdateRules([]C.Rule{RC.NewMatch("REJECT")}, map[string][]C.Rule{
		"named-direct": {
			RC.NewDomainSuffix("cn", "DIRECT"),
			RC.NewMatch("REJECT"),
		},
	}, nil)

	metadata := C.Metadata{SpecialRules: "named-direct"}
	if !Tunnel.ClassifyKernelDirect(metadata, "example.cn", netip.MustParseAddr("203.0.113.14")) {
		t.Fatal("named TUN special rules were not used")
	}
}

func hijackedSmobaMetadata(src netip.Addr, network C.NetWork, inbound C.Type) *C.Metadata {
	return &C.Metadata{
		NetWork: network,
		Type:    inbound,
		SrcIP:   src,
		SrcPort: 54321,
		DstIP:   netip.MustParseAddr("36.155.199.151"),
		DstPort: 6651,
	}
}

func TestKernelDirectRejectsHijackedLocalOutbound(t *testing.T) {
	metadata := hijackedSmobaMetadata(netip.MustParseAddr("127.0.0.1"), C.TCP, C.REDIR)
	if !isHijackedLocalTCP(metadata) {
		t.Fatal("REDIR TCP sourced from a local address to a public dest must be rejected")
	}
	metadata.Type = C.TUN
	if !isHijackedLocalTCP(metadata) {
		t.Fatal("TUN TCP sourced from a local address to a public dest must be rejected")
	}
}

func TestKernelDirectAllowsLANClientToPublicDest(t *testing.T) {
	src := netip.MustParseAddr("192.168.1.128")
	if ok, err := iface.IsLocalIp(src); err != nil {
		t.Fatalf("IsLocalIp: %v", err)
	} else if ok {
		t.Skip("192.168.1.128 is a local interface on this host")
	}
	metadata := hijackedSmobaMetadata(src, C.TCP, C.REDIR)
	if isHijackedLocalTCP(metadata) {
		t.Fatal("LAN 192.168.1.0/24 client to a public dest must pass")
	}
}

func TestKernelDirectDoesNotRejectUDP(t *testing.T) {
	for _, port := range []uint16{500, 4500, 6651} {
		metadata := hijackedSmobaMetadata(netip.MustParseAddr("127.0.0.1"), C.UDP, C.TUN)
		metadata.DstPort = port
		if isHijackedLocalTCP(metadata) {
			t.Fatalf("UDP dest %d must not be rejected as a TCP self-hijack", port)
		}
	}
}

func TestKernelDirectDoesNotRejectSOCKSLocalhost(t *testing.T) {
	metadata := hijackedSmobaMetadata(netip.MustParseAddr("127.0.0.1"), C.TCP, C.SOCKS5)
	if isHijackedLocalTCP(metadata) {
		t.Fatal("explicit SOCKS from localhost is not a Redir/TUN self-hijack")
	}
}

func TestKernelDirectAllowsLocalOutboundToProxyDest(t *testing.T) {
	setupKernelDirectTest(t, []C.Rule{RC.NewMatch("REJECT")})
	metadata := hijackedSmobaMetadata(netip.MustParseAddr("127.0.0.1"), C.TCP, C.REDIR)
	if isHijackedLocalTCP(metadata) {
		t.Fatal("local outbound to a PROXY/REJECT dest must not be dropped as a DIRECT self-hijack")
	}
}

func TestKernelDirectDoesNotRejectPrivateDest(t *testing.T) {
	metadata := hijackedSmobaMetadata(netip.MustParseAddr("127.0.0.1"), C.TCP, C.REDIR)
	metadata.DstIP = netip.MustParseAddr("192.168.1.50")
	if isHijackedLocalTCP(metadata) {
		t.Fatal("local source to a private dest must pass")
	}
}

type stubRemoteAddr struct{ s string }

func (a stubRemoteAddr) Network() string { return "tcp" }
func (a stubRemoteAddr) String() string  { return a.s }

type stubRemoteConn struct {
	net.Conn
	remote net.Addr
}

func (c stubRemoteConn) RemoteAddr() net.Addr { return c.remote }

type groupAdapter struct {
	benchmarkAdapter
	name  string
	typ   C.AdapterType
	inner C.Proxy
}

func (g *groupAdapter) Name() string                     { return g.name }
func (g *groupAdapter) Type() C.AdapterType              { return g.typ }
func (g *groupAdapter) Unwrap(*C.Metadata, bool) C.Proxy { return g.inner }
func (g *groupAdapter) Adapter() C.ProxyAdapter          { return g }

func TestKernelDirectProxyIsDirectUnwrapsGroup(t *testing.T) {
	direct := &benchmarkAdapter{}
	group := &groupAdapter{name: "漏網之魚", typ: C.Selector, inner: direct}
	if !kernelDirectProxyIsDirect(&C.Metadata{}, group) {
		t.Fatal("selector currently on DIRECT must be treated as DIRECT")
	}

	nested := &groupAdapter{name: "節點選擇", typ: C.Selector, inner: group}
	if !kernelDirectProxyIsDirect(&C.Metadata{}, nested) {
		t.Fatal("nested selector on DIRECT must unwrap")
	}

	reject := &kernelDirectRejectAdapter{}
	proxied := &groupAdapter{name: "漏網之魚", typ: C.Selector, inner: reject}
	if kernelDirectProxyIsDirect(&C.Metadata{}, proxied) {
		t.Fatal("selector on REJECT must not be treated as DIRECT")
	}
}

func TestObserveFlowLearnsGroupSelectedDirect(t *testing.T) {
	var current kerneldirect.DecisionSets
	closer := kerneldirect.Register(func(string, netip.Addr) bool { return true }, func(sets kerneldirect.DecisionSets) {
		current = sets
	}, kerneldirect.ControllerOptions{MaxEntries: 8})
	t.Cleanup(func() { _ = closer.Close() })

	addr := netip.MustParseAddr("36.155.199.151")
	group := &groupAdapter{name: "漏網之魚", typ: C.Selector, inner: &benchmarkAdapter{}}
	observeKernelDirectFlow(&C.Metadata{Host: "iwx.smoba.qq.com", DstIP: addr, DstPort: 6651}, group)
	if current.Direct == nil || !current.Direct.Contains(addr) {
		t.Fatal("group selected DIRECT must enter the kernel-direct set")
	}
}

func TestObserveFlowSkipsGroupSelectedProxy(t *testing.T) {
	var current kerneldirect.DecisionSets
	closer := kerneldirect.Register(func(string, netip.Addr) bool { return true }, func(sets kerneldirect.DecisionSets) {
		current = sets
	}, kerneldirect.ControllerOptions{MaxEntries: 8})
	t.Cleanup(func() { _ = closer.Close() })

	addr := netip.MustParseAddr("36.155.199.151")
	group := &groupAdapter{name: "漏網之魚", typ: C.Selector, inner: &kernelDirectRejectAdapter{}}
	observeKernelDirectFlow(&C.Metadata{Host: "iwx.smoba.qq.com", DstIP: addr, DstPort: 6651}, group)
	if current.Direct != nil && current.Direct.Contains(addr) {
		t.Fatal("group selected PROXY must not be learned as DIRECT")
	}
}

func TestKernelDirectObservesAfterDialWhenDstIPMissing(t *testing.T) {
	var current kerneldirect.DecisionSets
	closer := kerneldirect.Register(func(string, netip.Addr) bool { return true }, func(sets kerneldirect.DecisionSets) {
		current = sets
	}, kerneldirect.ControllerOptions{MaxEntries: 8})
	t.Cleanup(func() { _ = closer.Close() })

	proxy := &benchmarkAdapter{}
	addr := netip.MustParseAddr("36.155.199.151")
	metadata := &C.Metadata{
		Host:    "iwx.smoba.qq.com",
		DstPort: 6651,
	}
	observeKernelDirectFlow(metadata, proxy)
	if current.Direct != nil && current.Direct.Contains(addr) {
		t.Fatal("must not observe a DIRECT dest before the address is known")
	}

	remote := stubRemoteConn{remote: stubRemoteAddr{s: net.JoinHostPort(addr.String(), "6651")}}
	observeKernelDirectFlowAfterDial(metadata, proxy, remote)
	if current.Direct == nil || !current.Direct.Contains(addr) {
		t.Fatal("successful DIRECT dial must observe the dest once the IP is known")
	}
	if metadata.DstIP != addr {
		t.Fatalf("metadata.DstIP = %s, want %s", metadata.DstIP, addr)
	}
}
