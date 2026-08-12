package tunnel

import (
	"net/netip"
	"testing"

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
