package provider

import (
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
)

func TestClassicalStrategyDomainSetFastPath(t *testing.T) {
	s := loadClassicalStrategy([]string{
		"DOMAIN,example.com",
		"DOMAIN-SUFFIX,example.org",
		"DOMAIN-KEYWORD,blocked",
		"DST-PORT,443",
		"IP-CIDR,10.0.0.0/8",
		"IP-CIDR,192.168.0.0/16,no-resolve",
	})
	if s.Count() != 6 {
		t.Fatalf("count=%d want 6", s.Count())
	}
	if s.domainSet == nil {
		t.Fatal("expected domain set fast path")
	}
	if len(s.rules) != 4 { // keyword + port + both CIDRs stay linear
		t.Fatalf("remaining rules=%d want 4", len(s.rules))
	}

	helper := C.RuleMatchHelper{}
	cases := []struct {
		name string
		md   *C.Metadata
		want bool
	}{
		{name: "exact domain", md: &C.Metadata{Host: "example.com"}, want: true},
		{name: "suffix apex", md: &C.Metadata{Host: "example.org"}, want: true},
		{name: "suffix child", md: &C.Metadata{Host: "www.example.org"}, want: true},
		{name: "keyword", md: &C.Metadata{Host: "ads.blocked.tld"}, want: true},
		{name: "port", md: &C.Metadata{DstPort: 443}, want: true},
		{name: "cidr after resolve skip", md: &C.Metadata{DstIP: netip.MustParseAddr("10.1.2.3")}, want: true},
		{name: "no-resolve cidr with ip", md: &C.Metadata{DstIP: netip.MustParseAddr("192.168.1.1")}, want: true},
		{name: "miss", md: &C.Metadata{Host: "safe.example.net", DstPort: 80, DstIP: netip.MustParseAddr("1.1.1.1")}, want: false},
	}
	for _, tc := range cases {
		if got := s.Match(tc.md, helper); got != tc.want {
			t.Fatalf("%s: match=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestClassicalStrategyDomainHitStillRunsProcessLookup(t *testing.T) {
	s := loadClassicalStrategy([]string{
		"DOMAIN,example.com",
		"PROCESS-NAME,chrome",
	})
	looked := false
	md := &C.Metadata{Host: "example.com"}
	helper := C.RuleMatchHelper{FindProcess: func() {
		looked = true
		md.Process = "firefox"
	}}
	if !s.Match(md, helper) {
		t.Fatal("expected DOMAIN hit")
	}
	if !looked {
		t.Fatal("DOMAIN hit skipped FindProcess on leftover PROCESS-NAME")
	}
}

func TestClassicalStrategyDomainHitStillResolvesIPRules(t *testing.T) {
	s := loadClassicalStrategy([]string{
		"DOMAIN,example.com",
		"IP-CIDR,10.0.0.0/8",
	})
	resolved := false
	md := &C.Metadata{Host: "example.com"}
	helper := C.RuleMatchHelper{ResolveIP: func() {
		resolved = true
		md.DstIP = netip.MustParseAddr("1.1.1.1")
	}}
	if !s.Match(md, helper) {
		t.Fatal("expected DOMAIN hit")
	}
	if !resolved {
		t.Fatal("DOMAIN hit skipped ResolveIP on leftover IP-CIDR")
	}
}

func TestClassicalStrategyNoResolveCidrDoesNotForceDNS(t *testing.T) {
	s := loadClassicalStrategy([]string{
		"IP-CIDR,10.0.0.0/8,no-resolve",
	})
	if s.domainSet != nil {
		t.Fatal("CIDR-only set should not build a domain fast path")
	}
	resolved := false
	helper := C.RuleMatchHelper{ResolveIP: func() { resolved = true }}
	if s.Match(&C.Metadata{Host: "example.com"}, helper) {
		t.Fatal("unresolved no-resolve CIDR should miss")
	}
	if resolved {
		t.Fatal("no-resolve CIDR should not invoke ResolveIP")
	}
}

func TestIPCidrStrategyResolveIPSideEffects(t *testing.T) {
	s := loadIPCidrStrategy(1)
	resolved := 0
	md := &C.Metadata{DstIP: netip.MustParseAddr("11.0.0.1")}
	helper := C.RuleMatchHelper{ResolveIP: func() { resolved++ }}
	_ = s.Match(md, helper)
	if resolved != 0 {
		t.Fatal("already-valid DstIP should not ResolveIP")
	}

	md = &C.Metadata{Host: "example.com"}
	helper = C.RuleMatchHelper{ResolveIP: func() {
		resolved++
		md.DstIP = netip.MustParseAddr("10.0.0.1")
	}}
	if !s.Match(md, helper) {
		t.Fatal("expected hit after resolve")
	}
	if resolved != 1 {
		t.Fatalf("ResolveIP calls=%d want 1", resolved)
	}

	empty := NewIPCidrStrategy()
	resolved = 0
	helper = C.RuleMatchHelper{ResolveIP: func() { resolved++ }}
	if empty.Match(&C.Metadata{Host: "example.com"}, helper) {
		t.Fatal("empty set should miss")
	}
	if resolved != 1 {
		t.Fatal("empty IPCIDR set must still ResolveIP for later rules")
	}
}

func TestRulesParseEmptyYAMLPayloadHead(t *testing.T) {
	strategy, err := rulesParse([]byte("payload:\n"), NewDomainStrategy(), P.YamlRule)
	if err != nil {
		t.Fatalf("empty payload head: %v", err)
	}
	if strategy.Count() != 0 {
		t.Fatalf("count=%d want 0", strategy.Count())
	}
}

func TestRulesParseMultilineYAML(t *testing.T) {
	strategy, err := rulesParse(
		[]byte("payload:\n  - example.com\n  - api.example.com\n"),
		NewDomainStrategy(),
		P.YamlRule,
	)
	if err != nil {
		t.Fatalf("parse multi-line YAML: %v", err)
	}
	if !strategy.Match(&C.Metadata{Host: "api.example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("multi-line YAML payload was not loaded")
	}
}

func TestRulesParseTextSkipsComments(t *testing.T) {
	strategy, err := rulesParse(
		[]byte("# comment\n// premium comment\nexample.com\n"),
		NewDomainStrategy(),
		P.TextRule,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strategy.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{}) {
		t.Fatal("text payload was not loaded")
	}
	if strategy.Count() != 1 {
		t.Fatalf("count=%d want 1", strategy.Count())
	}
}
