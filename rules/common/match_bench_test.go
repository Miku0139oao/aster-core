package common

import (
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func benchMetadataHost(host string) *C.Metadata {
	return &C.Metadata{Host: host}
}

func BenchmarkDomainMatch(b *testing.B) {
	rule := NewDomain("www.example.com", "DIRECT")
	hit := benchMetadataHost("www.example.com")
	miss := benchMetadataHost("cdn.example.net")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkDomainSuffixMatch(b *testing.B) {
	rule := NewDomainSuffix("example.com", "DIRECT")
	hitExact := benchMetadataHost("example.com")
	hitSub := benchMetadataHost("www.example.com")
	miss := benchMetadataHost("notexample.com")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hitExact, helper)
		_, _ = rule.Match(hitSub, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkDomainSuffixMatchMixedCase(b *testing.B) {
	rule := NewDomainSuffix("example.com", "DIRECT")
	hit := benchMetadataHost("WWW.Example.COM")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
	}
}

func BenchmarkNewDomainSuffix(b *testing.B) {
	var sink *DomainSuffix
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = NewDomainSuffix("example.com", "DIRECT")
	}
	if sink == nil {
		b.Fatal("nil")
	}
}

func BenchmarkDomainKeywordMatch(b *testing.B) {
	rule := NewDomainKeyword("google", "DIRECT")
	hit := benchMetadataHost("www.google.com")
	miss := benchMetadataHost("www.example.com")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkDomainKeywordMatchMixedCase(b *testing.B) {
	rule := NewDomainKeyword("google", "DIRECT")
	hit := benchMetadataHost("WWW.GOOGLE.COM")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
	}
}

func BenchmarkDomainWildcardMatch(b *testing.B) {
	rule, err := NewDomainWildcard("*.example.com", "DIRECT")
	if err != nil {
		b.Fatal(err)
	}
	hit := benchMetadataHost("www.example.com")
	miss := benchMetadataHost("example.net")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkDomainRegexMatch(b *testing.B) {
	rule, err := NewDomainRegex(`^.*\.example\.com$`, "DIRECT")
	if err != nil {
		b.Fatal(err)
	}
	hit := benchMetadataHost("www.example.com")
	miss := benchMetadataHost("example.net")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkDomainSuffixChain32(b *testing.B) {
	rules := make([]*DomainSuffix, 0, 32)
	for _, suffix := range []string{
		"google.com", "googleapis.com", "gstatic.com", "youtube.com",
		"facebook.com", "fbcdn.net", "twitter.com", "x.com",
		"github.com", "githubusercontent.com", "cloudflare.com", "cloudfront.net",
		"amazon.com", "amazonaws.com", "apple.com", "icloud.com",
		"microsoft.com", "live.com", "office.com", "windows.net",
		"netflix.com", "nflxvideo.net", "spotify.com", "reddit.com",
		"wikipedia.org", "wikimedia.org", "baidu.com", "qq.com",
		"tiktok.com", "bytedance.com", "example.org", "example.com",
	} {
		rules = append(rules, NewDomainSuffix(suffix, "PROXY"))
	}
	meta := benchMetadataHost("www.example.com")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, rule := range rules {
			if ok, _ := rule.Match(meta, helper); ok {
				break
			}
		}
	}
}

func BenchmarkPortMatch(b *testing.B) {
	rule, err := NewPort("443", "DIRECT", C.DstPort)
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{DstPort: 443}
	miss := &C.Metadata{DstPort: 80}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkIPCIDRMatch(b *testing.B) {
	rule, err := NewIPCIDR("10.0.0.0/8", "DIRECT", WithIPCIDRNoResolve(true))
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{DstIP: netip.MustParseAddr("10.1.2.3")}
	miss := &C.Metadata{DstIP: netip.MustParseAddr("1.1.1.1")}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkIPSuffixMatch(b *testing.B) {
	rule, err := NewIPSuffix("1.2.3.0/24", "DIRECT", false, true)
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{DstIP: netip.MustParseAddr("8.1.2.3")}
	miss := &C.Metadata{DstIP: netip.MustParseAddr("8.8.8.8")}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkProcessNameRegexMatch(b *testing.B) {
	rule, err := NewProcess(`chrome.*`, "DIRECT", C.ProcessNameRegex)
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{Process: "chrome.exe"}
	miss := &C.Metadata{Process: "firefox.exe"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkProcessNameMatch(b *testing.B) {
	rule, err := NewProcess("chrome.exe", "DIRECT", C.ProcessName)
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{Process: "chrome.exe"}
	miss := &C.Metadata{Process: "firefox.exe"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}

func BenchmarkProcessNameWildcardMatch(b *testing.B) {
	rule, err := NewProcess("chrome*", "DIRECT", C.ProcessNameWildcard)
	if err != nil {
		b.Fatal(err)
	}
	hit := &C.Metadata{Process: "chrome.exe"}
	miss := &C.Metadata{Process: "firefox.exe"}
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rule.Match(hit, helper)
		_, _ = rule.Match(miss, helper)
	}
}
