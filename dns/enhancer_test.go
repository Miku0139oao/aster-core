package dns

import (
	"net/netip"
	"testing"

	"github.com/Miku0139oao/aster-core/component/fakeip"
)

func newTestPool(t *testing.T, prefix string) *fakeip.Pool {
	t.Helper()
	pool, err := fakeip.New(fakeip.Options{IPNet: netip.MustParsePrefix(prefix), Size: 64})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestFindHostByIPResolvesInsideRange covers both the locked path (fake-ip
// enabled) and the fact that the fast-path range gate does not skip a pool
// that can actually hold the address.
func TestFindHostByIPResolvesInsideRange(t *testing.T) {
	pool := newTestPool(t, "198.18.0.0/16")
	h := NewEnhancer(EnhancerConfig{EnhancedMode: 1 /* DNSFakeIP set below */})
	h.mode = 3 // C.DNSFakeIP
	h.fakeIPPool = pool

	ip := pool.Lookup("example.com")
	if !pool.IPNet().Contains(ip) {
		t.Fatalf("pool handed out %v outside %v", ip, pool.IPNet())
	}
	host, ok := h.FindHostByIP(ip)
	if !ok || host != "example.com" {
		t.Fatalf("FindHostByIP(%v) = %q,%v; want example.com,true", ip, host, ok)
	}
}

// TestFindHostByIPTwoPoolsFiltersByRange pins the v4+v6 case: the fast path
// must not ask the wrong pool, and must still find the host in the right one.
func TestFindHostByIPTwoPoolsFiltersByRange(t *testing.T) {
	pool4 := newTestPool(t, "198.18.0.0/16")
	pool6 := newTestPool(t, "fc00::/18")
	h := NewEnhancer(EnhancerConfig{})
	h.fakeIPPool, h.fakeIPPool6 = pool4, pool6

	ip4 := pool4.Lookup("v4.example.com")
	ip6 := pool6.Lookup("v6.example.com")

	if host, ok := h.FindHostByIP(ip4); !ok || host != "v4.example.com" {
		t.Fatalf("v4 lookup = %q,%v", host, ok)
	}
	if host, ok := h.FindHostByIP(ip6); !ok || host != "v6.example.com" {
		t.Fatalf("v6 lookup = %q,%v", host, ok)
	}
	if _, ok := h.FindHostByIP(netip.MustParseAddr("192.0.2.1")); ok {
		t.Fatal("real address must not resolve through fake-ip pools")
	}
}

// TestFindHostByIPSinglePoolKeepsStaleEntry is the behaviour-preservation
// guard: with a single pool the range gate is skipped, so a mapping that is
// outside the pool's current range still resolves the way it always did.
func TestFindHostByIPSinglePoolKeepsStaleEntry(t *testing.T) {
	pool := newTestPool(t, "198.18.0.0/16")
	h := NewEnhancer(EnhancerConfig{EnhancedMode: 3})
	h.fakeIPPool = pool

	// Simulate a persisted entry from an older, different range.
	stale := netip.MustParseAddr("203.0.113.9")
	h.mapping.Set(stale, "stale.example.com")

	host, ok := h.FindHostByIP(stale)
	if !ok || host != "stale.example.com" {
		t.Fatalf("stale mapping = %q,%v; want stale.example.com,true", host, ok)
	}
}

func BenchmarkFindHostByIPRealAddress(b *testing.B) {
	pool := mustTestPool(b, "198.18.0.0/16")
	h := NewEnhancer(EnhancerConfig{})
	h.fakeIPPool, h.fakeIPPool6 = pool, mustTestPool(b, "fc00::/18")
	real := netip.MustParseAddr("192.0.2.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.FindHostByIP(real)
	}
}

func mustTestPool(b *testing.B, prefix string) *fakeip.Pool {
	b.Helper()
	pool, err := fakeip.New(fakeip.Options{IPNet: netip.MustParsePrefix(prefix), Size: 64})
	if err != nil {
		b.Fatal(err)
	}
	return pool
}

// BenchmarkFindHostByIPRealAddressParallel shows the contention the range gate
// removes: without it every UDP packet takes the fake-ip pool mutex even for a
// real destination address.
func BenchmarkFindHostByIPRealAddressParallel(b *testing.B) {
	pool := mustTestPool(b, "198.18.0.0/16")
	h := NewEnhancer(EnhancerConfig{})
	h.fakeIPPool, h.fakeIPPool6 = pool, mustTestPool(b, "fc00::/18")
	real := netip.MustParseAddr("192.0.2.1")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = h.FindHostByIP(real)
		}
	})
}
