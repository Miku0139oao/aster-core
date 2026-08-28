package provider

import (
	"bytes"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
	"github.com/Miku0139oao/aster-core/rules/common"
)

func benchParseRule(tp, payload, target string, params []string, _ map[string][]C.Rule) (C.Rule, error) {
	switch tp {
	case "DOMAIN":
		return common.NewDomain(payload, target), nil
	case "DOMAIN-SUFFIX":
		return common.NewDomainSuffix(payload, target), nil
	case "DOMAIN-KEYWORD":
		return common.NewDomainKeyword(payload, target), nil
	case "IP-CIDR", "IP-CIDR6":
		isSrc, noResolve := common.ParseParams(params)
		return common.NewIPCIDR(payload, target, common.WithIPCIDRSourceIP(isSrc), common.WithIPCIDRNoResolve(noResolve))
	case "DST-PORT":
		return common.NewPort(payload, target, C.DstPort)
	case "PROCESS-NAME":
		return common.NewProcess(payload, target, C.ProcessName)
	default:
		return nil, fmt.Errorf("unsupported bench rule type: %s", tp)
	}
}

func benchDomainLines(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "d%d.example.com\n", i)
	}
	return []byte(b.String())
}

func benchYAMLDomainLines(n int) []byte {
	var b strings.Builder
	b.WriteString("payload:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - d%d.example.com\n", i)
	}
	return []byte(b.String())
}

func benchClassicalDomainLines(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("DOMAIN,d%d.example.com", i)
	}
	return out
}

func loadDomainStrategy(n int) *domainStrategy {
	s := NewDomainStrategy()
	s.Reset()
	for i := 0; i < n; i++ {
		s.Insert(fmt.Sprintf("d%d.example.com", i))
	}
	s.FinishInsert()
	return s
}

func loadIPCidrStrategy(n int) *ipcidrStrategy {
	s := NewIPCidrStrategy()
	s.Reset()
	for i := 0; i < n; i++ {
		s.Insert(fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
	}
	s.FinishInsert()
	return s
}

func loadClassicalStrategy(rules []string) *classicalStrategy {
	s := NewClassicalStrategy(benchParseRule)
	s.Reset()
	for _, rule := range rules {
		s.Insert(rule)
	}
	s.FinishInsert()
	return s
}

func BenchmarkDomainStrategyMatchHit(b *testing.B) {
	s := loadDomainStrategy(2000)
	md := &C.Metadata{Host: "d1234.example.com"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !s.Match(md, helper) {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkDomainStrategyMatchMiss(b *testing.B) {
	s := loadDomainStrategy(2000)
	md := &C.Metadata{Host: "miss.example.org"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.Match(md, helper) {
			b.Fatal("expected miss")
		}
	}
}

func BenchmarkIPCidrStrategyMatchHit(b *testing.B) {
	s := loadIPCidrStrategy(2000)
	md := &C.Metadata{DstIP: netip.MustParseAddr("10.3.232.10")}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !s.Match(md, helper) {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkIPCidrStrategyMatchMiss(b *testing.B) {
	s := loadIPCidrStrategy(2000)
	md := &C.Metadata{DstIP: netip.MustParseAddr("11.0.0.1")}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.Match(md, helper) {
			b.Fatal("expected miss")
		}
	}
}

func BenchmarkClassicalStrategyMatchHit(b *testing.B) {
	s := loadClassicalStrategy(benchClassicalDomainLines(2000))
	md := &C.Metadata{Host: "d1234.example.com"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !s.Match(md, helper) {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkClassicalStrategyMatchMiss(b *testing.B) {
	s := loadClassicalStrategy(benchClassicalDomainLines(2000))
	md := &C.Metadata{Host: "miss.example.org"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.Match(md, helper) {
			b.Fatal("expected miss")
		}
	}
}

func BenchmarkRulesParseText(b *testing.B) {
	buf := benchDomainLines(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rulesParse(buf, NewDomainStrategy(), P.TextRule); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRulesParseYAML(b *testing.B) {
	buf := benchYAMLDomainLines(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rulesParse(buf, NewDomainStrategy(), P.YamlRule); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertToMrsDomain(b *testing.B) {
	buf := benchDomainLines(2000)
	var out bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out.Reset()
		if err := ConvertToMrs(buf, P.Domain, P.TextRule, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRulesMrsParseDomain(b *testing.B) {
	var encoded bytes.Buffer
	if err := ConvertToMrs(benchDomainLines(2000), P.Domain, P.TextRule, &encoded); err != nil {
		b.Fatal(err)
	}
	raw := encoded.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rulesMrsParse(raw, NewDomainStrategy()); err != nil {
			b.Fatal(err)
		}
	}
}
