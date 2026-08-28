package strmatcher

import (
	"fmt"
	"testing"
)

func benchMphGroup(n int) *MphMatcherGroup {
	g := NewMphMatcherGroup()
	for i := 0; i < n; i++ {
		_, _ = g.AddPattern(fmt.Sprintf("example-%d.com", i), Domain)
		_, _ = g.AddPattern(fmt.Sprintf("full-%d.cdn.net", i), Full)
	}
	_, _ = g.AddPattern("tracker", Substr)
	g.Build()
	return g
}

func benchAC() *ACAutomaton {
	ac := NewACAutomaton()
	for _, p := range []string{
		"google", "facebook", "doubleclick", "analytics", "adservice",
		"googlesyndication", "googletagmanager", "facebook.net",
	} {
		if !ac.Add(p, Substr) {
			panic(p)
		}
	}
	for i := 0; i < 64; i++ {
		if !ac.Add(fmt.Sprintf("kw-%d", i), Substr) {
			panic(i)
		}
	}
	ac.Build()
	return ac
}

func BenchmarkMphMatchHit(b *testing.B) {
	g := benchMphGroup(2048)
	inputs := []string{
		"www.example-7.com",
		"a.b.example-100.com",
		"full-3.cdn.net",
		"cdn.example-0.com",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if len(g.Match(inputs[i%len(inputs)])) > 0 {
			hits++
		}
	}
	if hits == 0 {
		b.Fatal("expected hits")
	}
}

func BenchmarkMphMatchMiss(b *testing.B) {
	g := benchMphGroup(2048)
	inputs := []string{
		"www.not-listed.example",
		"cdn.unrelated.net",
		"miss.aster.test",
		"nope.local",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if len(g.Match(inputs[i%len(inputs)])) > 0 {
			hits++
		}
	}
	if hits != 0 {
		b.Fatal("expected misses")
	}
}

func BenchmarkACMatchHit(b *testing.B) {
	ac := benchAC()
	inputs := []string{
		"ads.google.com",
		"www.facebook.com",
		"page.analytics.example",
		"kw-12.cdn.net",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if ac.Match(inputs[i%len(inputs)]) {
			hits++
		}
	}
	if hits == 0 {
		b.Fatal("expected hits")
	}
}

func BenchmarkACMatchMiss(b *testing.B) {
	ac := benchAC()
	inputs := []string{
		"www.example.com",
		"cdn.aster.test",
		"miss.local",
		"nope.invalid",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var hits int
	for i := 0; i < b.N; i++ {
		if ac.Match(inputs[i%len(inputs)]) {
			hits++
		}
	}
	if hits != 0 {
		b.Fatal("expected misses")
	}
}
