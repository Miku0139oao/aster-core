package resolver

import (
	"net/netip"
	"testing"
)

func TestLookupIP4PDisabledLeavesIPv4Untouched(t *testing.T) {
	SetIP4PEnable(false)
	t.Cleanup(func() { SetIP4PEnable(false) })

	addr := netip.MustParseAddr("32.1.0.0")
	got, port := LookupIP4P(addr, "443")
	if got != addr || port != "443" {
		t.Fatalf("LookupIP4P(%s, 443) = %s, %s", addr, got, port)
	}
}

func TestLookupIP4PEnabledDoesNotPanicOnIPv4(t *testing.T) {
	SetIP4PEnable(true)
	t.Cleanup(func() { SetIP4PEnable(false) })

	addr := netip.MustParseAddr("32.1.0.0")
	got, port := LookupIP4P(addr, "443")
	if got != addr || port != "443" {
		t.Fatalf("LookupIP4P(%s, 443) = %s, %s; IPv4 must not be treated as IP4P", addr, got, port)
	}
}

func BenchmarkLookupIP4PEnabledNonIP4P(b *testing.B) {
	SetIP4PEnable(true)
	b.Cleanup(func() { SetIP4PEnable(false) })
	addr := netip.MustParseAddr("2001:db8::1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, port := LookupIP4P(addr, "443")
		if got != addr || port != "443" {
			b.Fatalf("LookupIP4P(%s) = %s:%s", addr, got, port)
		}
	}
}

func TestLookupIP4PConvertsIPv6Encoding(t *testing.T) {
	SetIP4PEnable(true)
	t.Cleanup(func() { SetIP4PEnable(false) })

	// 2001:0000:0000:0000:0000:01bb:c000:0201 -> 192.0.2.1:443
	addr := netip.MustParseAddr("2001:0:0:0:0:1bb:c000:201")
	got, port := LookupIP4P(addr, "80")
	want := netip.MustParseAddr("192.0.2.1")
	if got != want || port != "443" {
		t.Fatalf("LookupIP4P(%s) = %s:%s, want %s:443", addr, got, port, want)
	}
}
