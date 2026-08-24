package kerneldirect

import (
	"io"
	"math"
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/resolver"

	"go4.org/netipx"
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

func TestControllerRegisterMaxEntriesContract(t *testing.T) {
	tests := []struct {
		name       string
		options    []ControllerOptions
		maxEntries uint32
	}{
		{name: "omitted options", maxEntries: 4096},
		{name: "zero defaults", options: []ControllerOptions{{MaxEntries: 0}}, maxEntries: 4096},
		{name: "maximum accepted", options: []ControllerOptions{{MaxEntries: 65536}}, maxEntries: 65536},
		{name: "over maximum clamps as backstop", options: []ControllerOptions{{MaxEntries: 65537}}, maxEntries: 65536},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, test.options...).(*controller)
			defer c.Close()

			if status := c.status(); status.MaxEntries != test.maxEntries {
				t.Fatalf("Register MaxEntries status = %d, want %d", status.MaxEntries, test.maxEntries)
			}
			if want := uint64(test.maxEntries) * 4; c.maxRecords != want {
				t.Fatalf("Register maxRecords = %d, want %d for MaxEntries %d", c.maxRecords, want, test.maxEntries)
			}
		})
	}
}

func TestControllerStatusesOmitClosedControllers(t *testing.T) {
	if statuses := Statuses(); len(statuses) != 0 {
		t.Fatalf("Statuses() started with %d controllers, want 0", len(statuses))
	}

	first := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 101}).(*controller)
	second := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 202}).(*controller)
	defer first.Close()
	defer second.Close()

	statuses := Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses() returned %d controllers after two registrations, want 2: %+v", len(statuses), statuses)
	}
	maxEntries := make(map[uint32]bool, len(statuses))
	for _, status := range statuses {
		maxEntries[status.MaxEntries] = true
	}
	if !maxEntries[101] || !maxEntries[202] {
		t.Fatalf("Statuses() did not contain both controllers: %+v", statuses)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	statuses = Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses() returned %d controllers after closing one, want 1: %+v", len(statuses), statuses)
	}
	if statuses[0].MaxEntries != 202 {
		t.Fatalf("Statuses() retained the wrong controller after Close: %+v", statuses)
	}
}

func TestControllerBoundsLearnedAddressesWithSafeLRUEviction(t *testing.T) {
	var current DecisionSets
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "proxy.example"
	}, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 2}).(*controller)
	defer c.Close()

	first := netip.MustParseAddr("203.0.113.1")
	second := netip.MustParseAddr("203.0.113.2")
	third := netip.MustParseAddr("203.0.113.3")
	ObserveDNS("direct.example", []DNSAnswer{{Addr: first, TTL: time.Minute}})
	ObserveDNS("proxy.example", []DNSAnswer{{Addr: second, TTL: time.Minute}})
	// Refresh the first address so the proxy-only second address is the LRU.
	ObserveDNS("direct.example", []DNSAnswer{{Addr: first, TTL: time.Minute}})
	ObserveDNS("direct.example", []DNSAnswer{{Addr: third, TTL: time.Minute}})

	status := c.status()
	if status.LearnedAddresses != 2 || status.DirectAddresses != 2 || status.ProxyAddresses != 0 || status.LearnedDomains != 2 || status.Evictions != 1 {
		t.Fatalf("unexpected bounded-set status: %+v", status)
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("LearnedDomains = %d, but %d (address, host) records remain", status.LearnedDomains, actual)
	}
	if current.Direct.Contains(second) || current.Proxy.Contains(second) {
		t.Fatal("evicted address must fall back to the normal TUN path")
	}
	if !current.Direct.Contains(first) || !current.Direct.Contains(third) {
		t.Fatal("most recently used DIRECT addresses must remain learned")
	}
}

func TestObserveFlowLearnsPureIPDirect(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return true }, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 8}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("36.155.199.151")
	ObserveFlow("iwx.smoba.qq.com", addr, time.Minute)
	if current.Direct == nil || !current.Direct.Contains(addr) {
		t.Fatal("live DIRECT dest must enter the kernel-direct set")
	}
}

func TestObserveFlowSkipsFakeIP(t *testing.T) {
	// Use a globally-routable address so this fails if IsFakeIP is not consulted
	// (198.18.0.0/15 is already rejected by the static benchmarking range).
	fakeIP := netip.MustParseAddr("8.8.8.8")
	previousMapper := resolver.DefaultHostMapper
	resolver.DefaultHostMapper = testFakeIPMapper{fakeIP: fakeIP}
	t.Cleanup(func() { resolver.DefaultHostMapper = previousMapper })

	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {})
	defer c.Close()

	ObserveFlow("fake.example", fakeIP, time.Minute)
	if status := c.(*controller).status(); status.LearnedAddresses != 0 || status.LearnedDomains != 0 {
		t.Fatalf("fake-IP destination was learned: %+v", status)
	}
}

func TestObserveFlowSkipsPrivateAndNonGlobalAddresses(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {})
	defer c.Close()

	for _, addr := range []netip.Addr{
		netip.MustParseAddr("192.168.1.1"),
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("169.254.1.1"),
		netip.MustParseAddr("224.0.0.1"),
		netip.MustParseAddr("0.0.0.0"),
		netip.MustParseAddr("0.0.0.1"),
		netip.MustParseAddr("100.64.0.1"),
		netip.MustParseAddr("100.127.255.254"),
		netip.MustParseAddr("198.18.0.1"),
		netip.MustParseAddr("198.19.255.255"),
	} {
		ObserveFlow("unsafe.example", addr, time.Minute)
	}

	if status := c.(*controller).status(); status.LearnedAddresses != 0 || status.LearnedDomains != 0 {
		t.Fatalf("private or non-global destination was learned: %+v", status)
	}
}

type testFakeIPMapper struct {
	fakeIP netip.Addr
}

func (testFakeIPMapper) FakeIPEnabled() bool {
	return true
}

func (testFakeIPMapper) MappingEnabled() bool {
	return true
}

func (m testFakeIPMapper) IsFakeIP(addr netip.Addr) bool {
	return addr == m.fakeIP
}

func (testFakeIPMapper) IsFakeBroadcastIP(netip.Addr) bool {
	return false
}

func (testFakeIPMapper) IsExistFakeIP(netip.Addr) bool {
	return false
}

func (testFakeIPMapper) FindHostByIP(netip.Addr) (string, bool) {
	return "", false
}

func (testFakeIPMapper) FlushFakeIP() error {
	return nil
}

func (testFakeIPMapper) InsertHostByIP(netip.Addr, string) {}

func (testFakeIPMapper) StoreFakePoolState() {}

func TestControllerExpiresBeforeEvictingLiveAddress(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	expired := netip.MustParseAddr("203.0.113.10")
	live := netip.MustParseAddr("203.0.113.11")
	c.mu.Lock()
	c.records[expired] = &addressRecords{
		byHost:   map[string]record{"expired.example": {direct: true, expires: time.Now().Add(-time.Second)}},
		lastSeen: 1,
	}
	c.recordsLen = 1
	c.sequence = 1
	c.mu.Unlock()

	ObserveDNS("live.example", []DNSAnswer{{Addr: live, TTL: time.Minute}})
	status := c.status()
	if status.LearnedAddresses != 1 || status.LearnedDomains != 1 || status.Evictions != 0 {
		t.Fatalf("expired address should be removed without an eviction: %+v", status)
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("LearnedDomains = %d after expiry, but %d (address, host) records remain", status.LearnedDomains, actual)
	}
}

func TestControllerDomainBudgetNeverDropsProxyForDirect(t *testing.T) {
	var current DecisionSets
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "proxy.example"
	}, func(sets DecisionSets) { current = sets }, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.20")
	ObserveDNS("proxy.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	for _, host := range []string{"one.example", "two.example", "three.example", "four.example", "five.example"} {
		ObserveDNS(host, []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	}
	if current.Direct.Contains(addr) || !current.Proxy.Contains(addr) {
		t.Fatal("domain budget pressure must never discard a PROXY decision in favor of DIRECT")
	}
	status := c.status()
	if status.LearnedDomains != int(c.maxRecords) {
		t.Fatalf("LearnedDomains = %d, want the full bounded budget %d: %+v", status.LearnedDomains, c.maxRecords, status)
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("LearnedDomains = %d, but %d (address, host) records remain", status.LearnedDomains, actual)
	}
}

func TestControllerDomainBudgetCollapsesToProxy(t *testing.T) {
	var current DecisionSets
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "proxy.example"
	}, func(sets DecisionSets) { current = sets }, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.30")
	for _, host := range []string{"one.example", "two.example", "three.example", "four.example"} {
		ObserveDNS(host, []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	}
	if !current.Direct.Contains(addr) || current.Proxy.Contains(addr) {
		t.Fatal("address should start as DIRECT after filling the domain budget")
	}
	ObserveDNS("proxy.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	if current.Direct.Contains(addr) || !current.Proxy.Contains(addr) {
		t.Fatal("a new PROXY host must collapse a full DIRECT address to PROXY")
	}
	status := c.status()
	if status.LearnedDomains != 1 || status.ProxyAddresses != 1 || status.DirectAddresses != 0 {
		t.Fatalf("collapsed address should keep one PROXY record: %+v", status)
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("LearnedDomains = %d after collapse, but %d records remain", status.LearnedDomains, actual)
	}
}

func TestControllerProxyCollapseKeepsLongerLivedProxy(t *testing.T) {
	var current DecisionSets
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "p1.example" && host != "p2.example"
	}, func(sets DecisionSets) { current = sets }, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.70")
	ObserveDNS("p1.example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	for _, host := range []string{"d1.example", "d2.example", "d3.example"} {
		ObserveDNS(host, []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	}
	ObserveDNS("p2.example", []DNSAnswer{{Addr: addr, TTL: time.Second}})
	if current.Direct.Contains(addr) || !current.Proxy.Contains(addr) {
		t.Fatal("address must stay PROXY after a full-budget PROXY observation")
	}
	c.mu.Lock()
	if _, kept := c.records[addr].byHost["p1.example"]; !kept {
		c.mu.Unlock()
		t.Fatal("collapse must not discard a still-valid PROXY host")
	}
	if rec, ok := c.records[addr].byHost["p2.example"]; ok {
		rec.expires = time.Now().Add(-time.Second)
		c.records[addr].byHost["p2.example"] = rec
	}
	c.mu.Unlock()

	ObserveDNS("d4.example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	if current.Direct.Contains(addr) || !current.Proxy.Contains(addr) {
		t.Fatal("expiring the short-lived PROXY host must not expose leftover DIRECT")
	}
	c.mu.Lock()
	_, kept := c.records[addr].byHost["p1.example"]
	c.mu.Unlock()
	if !kept {
		t.Fatal("longer-lived PROXY observation must survive p2 expiry")
	}
}

func TestControllerExpiresDomainRecordsBeforeRejectingNewHost(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.31")
	c.mu.Lock()
	c.records[addr] = &addressRecords{
		byHost: map[string]record{
			"one.example":   {direct: true, expires: time.Now().Add(-time.Second)},
			"two.example":   {direct: true, expires: time.Now().Add(time.Minute)},
			"three.example": {direct: true, expires: time.Now().Add(time.Minute)},
			"four.example":  {direct: true, expires: time.Now().Add(time.Minute)},
		},
		lastSeen: 4,
	}
	c.recordsLen = 4
	c.sequence = 4
	c.mu.Unlock()

	ObserveDNS("five.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	status := c.status()
	if status.LearnedDomains != 4 || status.Evictions != 0 {
		t.Fatalf("expired host should free a domain slot without eviction: %+v", status)
	}
	c.mu.Lock()
	_, added := c.records[addr].byHost["five.example"]
	_, expired := c.records[addr].byHost["one.example"]
	c.mu.Unlock()
	if !added || expired {
		t.Fatal("expired domain record must be replaced by the new host")
	}
}

func TestControllerEvictsOldestMultiHostAddress(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return true }, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 2}).(*controller)
	defer c.Close()

	old := netip.MustParseAddr("203.0.113.40")
	kept := netip.MustParseAddr("203.0.113.41")
	added := netip.MustParseAddr("203.0.113.42")
	ObserveDNS("old-a.example", []DNSAnswer{{Addr: old, TTL: time.Minute}})
	ObserveDNS("old-b.example", []DNSAnswer{{Addr: old, TTL: time.Minute}})
	ObserveDNS("kept.example", []DNSAnswer{{Addr: kept, TTL: time.Minute}})
	ObserveDNS("new.example", []DNSAnswer{{Addr: added, TTL: time.Minute}})

	status := c.status()
	if status.LearnedAddresses != 2 || status.LearnedDomains != 2 || status.Evictions != 1 {
		t.Fatalf("unexpected multi-host eviction status: %+v", status)
	}
	if current.Direct.Contains(old) {
		t.Fatal("oldest multi-host address must be evicted as a whole")
	}
	if !current.Direct.Contains(kept) || !current.Direct.Contains(added) {
		t.Fatal("newer addresses must remain after evicting the multi-host LRU")
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("recordsLen drifted after multi-host eviction: status=%d actual=%d", status.LearnedDomains, actual)
	}
}

func TestControllerSequenceWrapKeepsOldestEviction(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return true }, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 2}).(*controller)
	defer c.Close()

	older := netip.MustParseAddr("203.0.113.50")
	newer := netip.MustParseAddr("203.0.113.51")
	incoming := netip.MustParseAddr("203.0.113.52")
	c.mu.Lock()
	c.records[older] = &addressRecords{byHost: map[string]record{"older.example": {direct: true, expires: time.Now().Add(time.Minute)}}, lastSeen: 1}
	c.records[newer] = &addressRecords{byHost: map[string]record{"newer.example": {direct: true, expires: time.Now().Add(time.Minute)}}, lastSeen: math.MaxUint64}
	c.recordsLen = 2
	c.sequence = math.MaxUint64
	c.mu.Unlock()

	ObserveDNS("incoming.example", []DNSAnswer{{Addr: incoming, TTL: time.Minute}})
	if current.Direct.Contains(older) {
		t.Fatal("sequence wrap must not evict the newer address before the older one")
	}
	if !current.Direct.Contains(newer) || !current.Direct.Contains(incoming) {
		t.Fatal("after wrap, the newest and previously-newest addresses must remain")
	}
}

func TestControllerStatusesReportLearnedSetAndEvictions(t *testing.T) {
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "proxy.example"
	}, func(DecisionSets) {}, ControllerOptions{MaxEntries: 2}).(*controller)
	defer c.Close()

	ObserveDNS("direct.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.60"), TTL: time.Minute}})
	ObserveDNS("proxy.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.61"), TTL: time.Minute}})
	ObserveDNS("direct.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.60"), TTL: time.Minute}})
	ObserveDNS("extra.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.62"), TTL: time.Minute}})

	statuses := Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses() = %d, want 1", len(statuses))
	}
	status := statuses[0]
	if status.LearnedAddresses != 2 || status.DirectAddresses != 2 || status.ProxyAddresses != 0 || status.Evictions != 1 {
		t.Fatalf("populated Statuses() snapshot is wrong: %+v", status)
	}
	if status.LearnedDomains != countLearnedDomains(c) {
		t.Fatalf("Statuses LearnedDomains = %d, actual records = %d", status.LearnedDomains, countLearnedDomains(c))
	}
}

func TestControllerConcurrentObserveFlushStatuses(t *testing.T) {
	c := Register(func(host string, _ netip.Addr) bool {
		return host != "proxy.example"
	}, func(DecisionSets) {}, ControllerOptions{MaxEntries: 8}).(*controller)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			addr := netip.AddrFrom4([4]byte{203, 0, 113, byte(100 + i)})
			host := "host.example"
			if i%2 == 0 {
				host = "proxy.example"
			}
			for n := 0; n < 40; n++ {
				ObserveDNS(host, []DNSAnswer{{Addr: addr, TTL: time.Minute}})
				_ = Statuses()
				if n%10 == 0 {
					Flush()
				}
			}
		}(i)
	}
	wg.Wait()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if statuses := Statuses(); len(statuses) != 0 {
		t.Fatalf("Statuses() after Close = %d, want 0", len(statuses))
	}
}

func countLearnedDomains(c *controller) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for _, address := range c.records {
		count += len(address.byHost)
	}
	return count
}

func TestControllerSinkCanCallStatuses(t *testing.T) {
	called := false
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		called = true
		Statuses()
	}, ControllerOptions{MaxEntries: 10}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("8.8.4.4")
	ObserveDNS("direct.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	if !called {
		t.Fatal("sink was not called")
	}
}

func TestControllerPublishLoopSkipsStaleGeneration(t *testing.T) {
	var mu sync.Mutex
	var lastDirect bool
	c := Register(func(string, netip.Addr) bool { return true }, func(sets DecisionSets) {
		addr := netip.MustParseAddr("203.0.113.80")
		mu.Lock()
		lastDirect = sets.Direct != nil && sets.Direct.Contains(addr)
		mu.Unlock()
	}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.80")
	ObserveDNS("direct.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	mu.Lock()
	if !lastDirect {
		t.Fatal("initial DIRECT observation was not applied")
	}
	mu.Unlock()

	c.mu.Lock()
	c.generation++
	staleGeneration := c.generation - 1
	c.mu.Unlock()

	done := make(chan struct{})
	c.publishReqs <- publishRequest{
		sets:       DecisionSets{Direct: &netipx.IPSet{}, Proxy: &netipx.IPSet{}},
		generation: staleGeneration,
		done:       done,
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stale publish was not consumed")
	}

	mu.Lock()
	applied := lastDirect
	mu.Unlock()
	if !applied {
		t.Fatal("stale empty DecisionSets overwrote the current DIRECT exclude set")
	}
}

func TestControllerIgnoresSharedAndFakeIPRange(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {})
	defer c.Close()

	for _, addr := range []netip.Addr{
		netip.MustParseAddr("100.64.0.1"),
		netip.MustParseAddr("100.127.255.254"),
		netip.MustParseAddr("198.18.0.1"),
		netip.MustParseAddr("198.19.255.255"),
		netip.MustParseAddr("0.0.0.1"),
	} {
		ObserveDNS("unsafe.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	}

	if status := c.(*controller).status(); status.LearnedAddresses != 0 || status.LearnedDomains != 0 {
		t.Fatalf("CGNAT, fake-IP range, or this-network destination was learned: %+v", status)
	}
}

func TestControllerCloseUnblocksQueuedPublish(t *testing.T) {
	firstStarted := make(chan struct{})
	blockFirst := make(chan struct{})
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		select {
		case firstStarted <- struct{}{}:
			<-blockFirst
		default:
		}
	})

	addr := netip.MustParseAddr("203.0.113.81")
	go ObserveDNS("first.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first sink did not start")
	}

	queued := make(chan struct{})
	go func() {
		Flush()
		close(queued)
	}()

	// Flush bumps generation once it has cleared the learned set and is blocked
	// in publish behind the in-flight sink. Waiting on that is deterministic;
	// publishReqs is unbuffered so len(publishReqs) cannot observe the sender.
	ctrl := c.(*controller)
	deadline := time.Now().Add(2 * time.Second)
	for {
		ctrl.mu.Lock()
		gen := ctrl.generation
		ctrl.mu.Unlock()
		if gen >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Flush did not queue behind the blocked sink")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan struct{})
	go func() {
		_ = c.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung behind a queued publish")
	}

	close(blockFirst)
	select {
	case <-queued:
	case <-time.After(2 * time.Second):
		t.Fatal("queued Flush deadlocked after Close dropped its publish")
	}
}

func TestControllerCloseFromSinkDoesNotDeadlock(t *testing.T) {
	var c io.Closer
	c = Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		_ = c.Close()
	})
	defer func() { _ = c.Close() }()

	done := make(chan struct{})
	go func() {
		addr := netip.MustParseAddr("8.8.4.4")
		ObserveDNS("direct.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close from sink deadlocked")
	}
}

func TestControllerConcurrentCloseNeverStrandsObserveDNS(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 32})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			addr := netip.AddrFrom4([4]byte{203, 0, 113, byte(i + 1)})
			for n := 0; n < 40; n++ {
				ObserveDNS("close.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
				if n%5 == 0 {
					Flush()
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	time.Sleep(5 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveDNS/Flush stranded after Close")
	}
}

func TestControllerKeepsLongerLivedProxyUnderHostBudget(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return false }, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.77")
	ObserveDNS("long.example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	for i := 0; i < int(c.maxRecords)-1; i++ {
		ObserveDNS("fill-"+strconv.Itoa(i)+".example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	}
	ObserveDNS("short.example", []DNSAnswer{{Addr: addr, TTL: time.Second}})

	c.mu.Lock()
	_, kept := c.records[addr].byHost["long.example"]
	_, added := c.records[addr].byHost["short.example"]
	recordsLen := c.recordsLen
	c.mu.Unlock()
	if !kept {
		t.Fatal("host budget must not discard a longer-lived PROXY for a shorter incoming PROXY")
	}
	if added {
		t.Fatal("shorter incoming PROXY must not replace the longer-lived record")
	}
	if recordsLen != c.maxRecords {
		t.Fatalf("recordsLen = %d after rejected short PROXY, want bounded budget %d", recordsLen, c.maxRecords)
	}
	if actual := countLearnedDomains(c); actual != int(recordsLen) {
		t.Fatalf("recordsLen = %d, but %d (address, host) records remain", recordsLen, actual)
	}
	if current.Proxy == nil || !current.Proxy.Contains(addr) {
		t.Fatal("address must remain PROXY")
	}
}

func TestControllerReplacesShorterLivedProxyUnderHostBudget(t *testing.T) {
	var current DecisionSets
	c := Register(func(string, netip.Addr) bool { return false }, func(sets DecisionSets) {
		current = sets
	}, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.78")
	ObserveDNS("short.example", []DNSAnswer{{Addr: addr, TTL: time.Second}})
	for i := 0; i < int(c.maxRecords)-1; i++ {
		ObserveDNS("fill-"+strconv.Itoa(i)+".example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})
	}
	ObserveDNS("long.example", []DNSAnswer{{Addr: addr, TTL: time.Hour}})

	c.mu.Lock()
	_, dropped := c.records[addr].byHost["short.example"]
	_, added := c.records[addr].byHost["long.example"]
	recordsLen := c.recordsLen
	c.mu.Unlock()
	if dropped {
		t.Fatal("host budget should drop the soonest-expiring PROXY for a longer incoming PROXY")
	}
	if !added {
		t.Fatal("longer incoming PROXY must be recorded")
	}
	if recordsLen != c.maxRecords {
		t.Fatalf("recordsLen = %d after replacing short PROXY, want bounded budget %d", recordsLen, c.maxRecords)
	}
	if actual := countLearnedDomains(c); actual != int(recordsLen) {
		t.Fatalf("recordsLen = %d, but %d (address, host) records remain", recordsLen, actual)
	}
	if current.Proxy == nil || !current.Proxy.Contains(addr) {
		t.Fatal("address must remain PROXY")
	}
}

func TestControllerDoesNotLearnObservationExpiredDuringClassification(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	addr := netip.MustParseAddr("203.0.113.87")

	var mu sync.Mutex
	updates := 0
	published := false
	c := Register(func(string, netip.Addr) bool {
		close(started)
		<-release
		return true
	}, func(sets DecisionSets) {
		mu.Lock()
		updates++
		published = published || sets.Direct.Contains(addr) || sets.Proxy.Contains(addr)
		mu.Unlock()
	}, ControllerOptions{MaxEntries: 8}).(*controller)
	defer c.Close()
	defer close(release)

	done := make(chan struct{})
	go func() {
		ObserveDNS("expired.example", []DNSAnswer{{Addr: addr, TTL: minimumTTL}})
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("classifier did not start")
	}

	// The minimum accepted TTL is one second. Start this bounded wait only
	// after the classifier has received the observation, then release it with
	// enough margin to guarantee that the observation's absolute expiry passed.
	timer := time.NewTimer(minimumTTL + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for observation to expire")
	}
	release <- struct{}{}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveDNS did not return after classifier was released")
	}

	mu.Lock()
	gotUpdates := updates
	gotPublished := published
	mu.Unlock()
	if gotUpdates != 0 || gotPublished {
		t.Fatalf("expired observation was published: updates=%d containsAddress=%t", gotUpdates, gotPublished)
	}
	if status := c.status(); status.LearnedAddresses != 0 || status.LearnedDomains != 0 {
		t.Fatalf("expired observation was learned: %+v", status)
	}
}

func TestControllerFlushDiscardsInFlightClassification(t *testing.T) {
	var mu sync.Mutex
	var current DecisionSets
	inClassifier := make(chan struct{})
	release := make(chan struct{})
	c := Register(func(string, netip.Addr) bool {
		select {
		case inClassifier <- struct{}{}:
			<-release
		default:
		}
		return true
	}, func(sets DecisionSets) {
		mu.Lock()
		current = sets
		mu.Unlock()
	}, ControllerOptions{MaxEntries: 8})
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.88")
	done := make(chan struct{})
	go func() {
		ObserveDNS("stale.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
		close(done)
	}()
	select {
	case <-inClassifier:
	case <-time.After(2 * time.Second):
		t.Fatal("classifier did not start")
	}
	Flush()
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stale ObserveDNS did not return after Flush")
	}

	mu.Lock()
	learned := current.Direct != nil && current.Direct.Contains(addr)
	mu.Unlock()
	if learned {
		t.Fatal("observation classified before Flush must not be learned")
	}
	if status := c.(*controller).status(); status.LearnedAddresses != 0 || status.LearnedDomains != 0 {
		t.Fatalf("stale observation leaked into cache: %+v", status)
	}

	ObserveDNS("fresh.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	mu.Lock()
	learned = current.Direct != nil && current.Direct.Contains(addr)
	mu.Unlock()
	if !learned {
		t.Fatal("observation after Flush must be learned")
	}
}

func TestControllerHealsDriftedRecordCount(t *testing.T) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 1}).(*controller)
	defer c.Close()

	c.mu.Lock()
	c.recordsLen = c.maxRecords
	c.mu.Unlock()

	addr := netip.MustParseAddr("203.0.113.99")
	ObserveDNS("heal.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	status := c.status()
	if status.LearnedAddresses != 1 || status.LearnedDomains != 1 {
		t.Fatalf("drifted recordsLen must not block new observations: %+v", status)
	}
	if actual := countLearnedDomains(c); actual != status.LearnedDomains {
		t.Fatalf("LearnedDomains = %d, but %d (address, host) records remain", status.LearnedDomains, actual)
	}
	c.mu.Lock()
	recordsLen := c.recordsLen
	c.mu.Unlock()
	if recordsLen != 1 {
		t.Fatalf("recordsLen = %d after healing empty-map drift, want 1", recordsLen)
	}
}

func TestControllerClassifierCanCallStatuses(t *testing.T) {
	started := make(chan struct{})
	c := Register(func(string, netip.Addr) bool {
		select {
		case started <- struct{}{}:
		default:
		}
		_ = Statuses()
		return true
	}, func(DecisionSets) {}, ControllerOptions{MaxEntries: 8})
	defer c.Close()

	done := make(chan struct{})
	go func() {
		ObserveDNS("direct.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.8"), TTL: time.Minute}})
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("classifier did not run")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveDNS deadlocked because classifier called Statuses")
	}
}

func TestControllerSerializesClassifierCallbacks(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	c := Register(func(string, netip.Addr) bool {
		entered <- struct{}{}
		<-release
		return true
	}, func(DecisionSets) {}, ControllerOptions{MaxEntries: 8})
	defer c.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	for i, host := range []string{"first.example", "second.example"} {
		go func(index int, host string) {
			defer wg.Done()
			ObserveDNS(host, []DNSAnswer{{Addr: netip.AddrFrom4([4]byte{203, 0, 113, byte(20 + index)}), TTL: time.Minute}})
		}(i, host)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first classifier did not start")
	}
	select {
	case <-entered:
		t.Fatal("classifier callbacks overlapped")
	case <-time.After(100 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("second classifier did not start after release")
	}
	release <- struct{}{}
	wg.Wait()
}

func TestControllerCloseFencesClassifier(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	c := Register(func(string, netip.Addr) bool {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return true
	}, func(DecisionSets) {}, ControllerOptions{MaxEntries: 8}).(*controller)

	observed := make(chan struct{})
	go func() {
		c.observe("blocked.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.30"), TTL: time.Minute}})
		close(observed)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("classifier did not start")
	}
	closed := make(chan struct{})
	go func() {
		_ = c.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while classifier was running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after classifier completed")
	}
	<-observed

	c.observe("after-close.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.31"), TTL: time.Minute}})
	if calls.Load() != 1 {
		t.Fatalf("classifier ran after Close: calls=%d", calls.Load())
	}
}

func TestControllerCloseUnblocksAcceptedPublish(t *testing.T) {
	inSink := make(chan struct{})
	block := make(chan struct{})
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		select {
		case inSink <- struct{}{}:
			<-block
		default:
		}
	})

	observed := make(chan struct{})
	go func() {
		ObserveDNS("direct.example", []DNSAnswer{{Addr: netip.MustParseAddr("203.0.113.91"), TTL: time.Minute}})
		close(observed)
	}()
	select {
	case <-inSink:
	case <-time.After(2 * time.Second):
		t.Fatal("sink did not start")
	}

	closed := make(chan struct{})
	go func() {
		_ = c.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung behind an accepted sink")
	}
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveDNS stayed blocked on the accepted publish after Close")
	}
	close(block)
}

func TestControllerRefreshWaitsForInFlightSink(t *testing.T) {
	var mu sync.Mutex
	applied := 0
	inFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {
		mu.Lock()
		applied++
		n := applied
		mu.Unlock()
		if n == 1 {
			close(inFirst)
			<-releaseFirst
		}
	})
	defer c.Close()

	addr := netip.MustParseAddr("203.0.113.92")
	go ObserveDNS("direct.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	select {
	case <-inFirst:
	case <-time.After(2 * time.Second):
		t.Fatal("first sink did not start")
	}

	second := make(chan struct{})
	go func() {
		ObserveDNS("direct.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
		close(second)
	}()
	select {
	case <-second:
		t.Fatal("identical ObserveDNS returned before the in-flight sink applied")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("identical ObserveDNS did not return after the sink applied")
	}
}

func TestHasConsumersTracksControllerLifecycle(t *testing.T) {
	before := activeControllers.Load()
	closer := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {})
	if got := activeControllers.Load(); got != before+1 || !HasConsumers() {
		t.Fatalf("active controllers = %d, before=%d, HasConsumers=%v", got, before, HasConsumers())
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if got := activeControllers.Load(); got != before {
		t.Fatalf("active controllers after close = %d, want %d", got, before)
	}
}

func TestWaitAppliedSharesBarrierPerGeneration(t *testing.T) {
	c := &controller{generation: 1, stop: make(chan struct{})}
	const waiters = 64
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			c.waitApplied(1)
		}()
	}

	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		count := len(c.applyWaiters)
		c.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiter barrier count = %d, want 1", count)
		}
		runtime.Gosched()
	}

	c.mu.Lock()
	c.applied = 1
	c.releaseWaitersLocked()
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shared apply barrier did not release all callers")
	}
}

func BenchmarkObserveFlowRefresh(b *testing.B) {
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 256})
	defer c.Close()

	addrs := make([]netip.Addr, 256)
	for i := range addrs {
		addrs[i] = netip.AddrFrom4([4]byte{203, 0, byte(i / 256), byte(i)})
		ObserveFlow("bench.example", addrs[i], time.Minute)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ObserveFlow("bench.example", addrs[i%len(addrs)], time.Minute)
	}
}
