package kerneldirect

import (
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestControllerProxyWinsAndFlushes(t *testing.T) {
	var mu sync.Mutex
	var current DecisionSets
	directHost := "direct.example"
	c := Register(func(host string, _ netip.Addr) bool {
		return host == directHost
	}, func(sets DecisionSets) {
		mu.Lock()
		current = sets
		mu.Unlock()
	})
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.9")
	ObserveDNS(directHost, []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	mu.Lock()
	if current.Direct == nil || !current.Direct.Contains(addr) || current.Proxy.Contains(addr) {
		t.Fatal("DIRECT answer was not added")
	}
	mu.Unlock()

	ObserveDNS("proxy.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	mu.Lock()
	if current.Direct.Contains(addr) || !current.Proxy.Contains(addr) {
		t.Fatal("shared proxy answer must move address from DIRECT to PROXY")
	}
	mu.Unlock()

	Flush()
	mu.Lock()
	if current.Direct == nil || current.Proxy == nil || len(current.Direct.Prefixes()) != 0 || len(current.Proxy.Prefixes()) != 0 {
		t.Fatal("flush did not clear learned addresses")
	}
	mu.Unlock()
}

func TestControllerIgnoresNonGlobalAddress(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return true }, func(sets DecisionSets) {
		current = sets
	})
	defer c.Close()

	ObserveDNS("router.example", []DNSAnswer{{Addr: netip.MustParseAddr("192.168.1.1"), TTL: time.Minute}})
	if current.Direct != nil && (len(current.Direct.Prefixes()) != 0 || len(current.Proxy.Prefixes()) != 0) {
		t.Fatal("private address must not enter the dynamic set")
	}
}

func TestControllerDoesNotRewriteUnchangedSet(t *testing.T) {
	updates := 0
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		updates++
	})
	defer c.Close()

	answer := []DNSAnswer{{Addr: netip.MustParseAddr("8.8.4.4"), TTL: time.Minute}}
	ObserveDNS("direct.example", answer)
	ObserveDNS("direct.example", answer)
	if updates != 1 {
		t.Fatalf("unchanged DNS answer caused %d nft set updates, want 1", updates)
	}
}
