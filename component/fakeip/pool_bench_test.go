package fakeip

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func benchPool(b *testing.B, n int) *Pool {
	b.Helper()
	pool, err := New(Options{
		IPNet: netip.MustParsePrefix("198.18.0.0/15"),
		Size:  n,
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n/2; i++ {
		pool.Lookup(fmt.Sprintf("host-%d.example.com", i))
	}
	return pool
}

func BenchmarkPoolLookupHit(b *testing.B) {
	pool := benchPool(b, 1024)
	hosts := make([]string, 256)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("host-%d.example.com", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Lookup(hosts[i%len(hosts)])
	}
}

func BenchmarkPoolLookupHitParallel(b *testing.B) {
	pool := benchPool(b, 1024)
	hosts := make([]string, 256)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("host-%d.example.com", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = pool.Lookup(hosts[i%len(hosts)])
			i++
		}
	})
}

func BenchmarkPoolLookBackHit(b *testing.B) {
	pool := benchPool(b, 1024)
	ips := make([]netip.Addr, 256)
	for i := range ips {
		ips[i] = pool.Lookup(fmt.Sprintf("host-%d.example.com", i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.LookBack(ips[i%len(ips)])
	}
}

type benchHostMatcher struct {
	hosts map[string]struct{}
}

func (m benchHostMatcher) MatchDomain(domain string) bool {
	_, ok := m.hosts[strings.ToLower(domain)]
	return ok
}

func BenchmarkSkipperHostHit(b *testing.B) {
	hosts := make(map[string]struct{}, 256)
	for i := 0; i < 256; i++ {
		hosts[fmt.Sprintf("skip-%d.example.com", i)] = struct{}{}
	}
	skipper := &Skipper{Host: []C.DomainMatcher{benchHostMatcher{hosts: hosts}}}
	queries := []string{
		"skip-1.example.com",
		"skip-40.example.com",
		"skip-200.example.com",
		"skip-7.example.com",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if skipper.ShouldSkipped(queries[i%len(queries)]) {
			hits++
		}
	}
	if hits == 0 {
		b.Fatal("expected hits")
	}
}

func BenchmarkSkipperHostMiss(b *testing.B) {
	hosts := make(map[string]struct{}, 256)
	for i := 0; i < 256; i++ {
		hosts[fmt.Sprintf("skip-%d.example.com", i)] = struct{}{}
	}
	skipper := &Skipper{Host: []C.DomainMatcher{benchHostMatcher{hosts: hosts}}}
	queries := []string{
		"www.google.com",
		"cdn.aster.test",
		"miss.local",
		"nope.invalid",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if skipper.ShouldSkipped(queries[i%len(queries)]) {
			hits++
		}
	}
	if hits != 0 {
		b.Fatal("expected misses")
	}
}

type benchDomainRule struct {
	host   string
	action string
}

func (r benchDomainRule) RuleType() C.RuleType { return C.Domain }
func (r benchDomainRule) Match(metadata *C.Metadata, _ C.RuleMatchHelper) (bool, string) {
	if metadata.Host == r.host {
		return true, r.action
	}
	return false, ""
}
func (r benchDomainRule) Adapter() string         { return r.action }
func (r benchDomainRule) Payload() string         { return r.host }
func (r benchDomainRule) ProviderNames() []string { return nil }

func BenchmarkSkipperRulesHit(b *testing.B) {
	rules := make([]C.Rule, 32)
	for i := range rules {
		rules[i] = benchDomainRule{
			host:   fmt.Sprintf("skip-%d.example.com", i),
			action: UseRealIP,
		}
	}
	skipper := &Skipper{Rules: rules}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if skipper.ShouldSkipped("skip-8.example.com") {
			hits++
		}
	}
	if hits == 0 {
		b.Fatal("expected hits")
	}
}
