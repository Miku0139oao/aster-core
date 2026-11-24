package router_test

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata/router"
)

// These domains are built in memory so the test does not need GeoSite.dat,
// which is not in the repository. They cover each lookup a domain matcher does:
// the trie-backed set (Full/Domain), the substring matcher (Plain), and the
// regex buckets keyed by TLD (Regex).
func caseDomains() []*router.Domain {
	return []*router.Domain{
		{Type: router.Domain_Full, Value: "full.example.com"},
		{Type: router.Domain_Domain, Value: "suffix.example.org"},
		{Type: router.Domain_Plain, Value: "keyword-tracker"},
		{Type: router.Domain_Regex, Value: `(^|\.)13mei[5-7]\.buzz$`},
		{Type: router.Domain_Regex, Value: `(^|\.)unbucketed\.[a-z]+$`},
		{Type: router.Domain_Full, Value: "中文.example"},
	}
}

type matcherCase struct {
	name string
	new  func([]*router.Domain) (router.DomainMatcher, error)
}

var matcherCases = []matcherCase{
	{"succinct", router.NewSuccinctMatcherGroup},
	{"mph", router.NewMphMatcherGroup},
}

// A mixed-case host must match the same lowercase rules as its lowercase form.
// The trie-backed DomainSet folds ASCII case during traversal, but the substring
// and regex matchers compare raw bytes, and the regex TLD bucket is keyed by the
// literal label.
func TestDomainMatchersFoldHostCase(t *testing.T) {
	hosts := []struct {
		host string
		want bool
	}{
		// trie set path: already case-folding, must stay that way.
		{"full.example.com", true},
		{"FULL.EXAMPLE.COM", true},
		{"WwW.Suffix.Example.ORG", true},
		{"NOT-FULL.EXAMPLE.NET", false},
		// substring (Domain_Plain) path.
		{"ads.keyword-tracker.net", true},
		{"ADS.KEYWORD-TRACKER.NET", true},
		// regex path with a TLD bucket: the bucket key and the regex are raw.
		{"13mei5.buzz", true},
		{"13MEI5.BUZZ", true},
		// regex path without an extractable TLD.
		{"www.unbucketed.xyz", true},
		{"WWW.UNBUCKETED.XYZ", true},
		// non-ASCII keys keep using the unicode path.
		{"中文.example", true},
		{"中文.Example", true},
		{"日本語.example", false},
		{"no-such-host.invalid", false},
	}

	for _, mc := range matcherCases {
		t.Run(mc.name, func(t *testing.T) {
			m, err := mc.new(caseDomains())
			if err != nil {
				t.Fatalf("build matcher: %v", err)
			}
			for _, tc := range hosts {
				if got := m.ApplyDomain(tc.host); got != tc.want {
					t.Errorf("ApplyDomain(%q) = %v, want %v", tc.host, got, tc.want)
				}
			}
		})
	}
}

// The fast path must not allocate when the host that reaches the case-sensitive
// matchers is already lowercase; mixed-case hosts may allocate once.
func BenchmarkSuccinctApplyDomainCase(b *testing.B) {
	m, err := router.NewSuccinctMatcherGroup(caseDomains())
	if err != nil {
		b.Fatal(err)
	}
	cases := map[string]string{
		"lowercase-miss":  "no-such-host.invalid",
		"mixed-case-miss": "NO-SUCH-HOST.INVALID",
		"hit-domain-set":  "www.suffix.example.org",
		"hit-keyword":     "ads.keyword-tracker.net",
	}
	for name, host := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.ApplyDomain(host)
			}
		})
	}
}
