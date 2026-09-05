package dns

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/resolver"

	D "github.com/miekg/dns"
)

type staticDNSClient struct {
	aDelay, aaaaDelay time.Duration
	a, aaaa           []net.IP
}

func (c *staticDNSClient) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	q := m.Question[0]
	var delay time.Duration
	var ips []net.IP
	switch q.Qtype {
	case D.TypeA:
		delay, ips = c.aDelay, c.a
	case D.TypeAAAA:
		delay, ips = c.aaaaDelay, c.aaaa
	default:
		msg := new(D.Msg)
		msg.SetRcode(m, D.RcodeNameError)
		return msg, nil
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	msg := new(D.Msg)
	msg.SetReply(m)
	for _, ip := range ips {
		switch q.Qtype {
		case D.TypeA:
			msg.Answer = append(msg.Answer, &D.A{
				Hdr: D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
				A:   ip,
			})
		case D.TypeAAAA:
			msg.Answer = append(msg.Answer, &D.AAAA{
				Hdr:  D.RR_Header{Name: q.Name, Rrtype: D.TypeAAAA, Class: D.ClassINET, Ttl: 60},
				AAAA: ip,
			})
		}
	}
	return msg, nil
}

func (c *staticDNSClient) Address() string { return "static" }

func (c *staticDNSClient) ResetConnection() {}

func testResolver(client dnsClient, ipv6Timeout time.Duration) *Resolver {
	return &Resolver{
		ipv6:        true,
		ipv6Timeout: ipv6Timeout,
		main:        []dnsClient{client},
		cache:       Config{}.newCache(),
	}
}

func TestLookupIPWaitsForAAAAWhenAFails(t *testing.T) {
	client := &staticDNSClient{
		aaaaDelay: 200 * time.Millisecond,
		aaaa:      []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)

	start := time.Now()
	ips, err := r.LookupIP(context.Background(), "ipv6-only.example")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LookupIP returned error: %v", err)
	}
	if len(ips) != 1 || ips[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("LookupIP ips = %v, want 2001:db8::1", ips)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("LookupIP returned too quickly (%v); AAAA should not be cut off by ipv6Timeout when A fails", elapsed)
	}
}

func TestLookupIPKeepsIPv6TimeoutAfterASucceeds(t *testing.T) {
	client := &staticDNSClient{
		a:         []net.IP{net.ParseIP("192.0.2.1").To4()},
		aaaaDelay: 200 * time.Millisecond,
		aaaa:      []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)

	start := time.Now()
	ips, err := r.LookupIP(context.Background(), "dual.example")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LookupIP returned error: %v", err)
	}
	if len(ips) != 1 || ips[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("LookupIP ips = %v, want only IPv4 when AAAA exceeds ipv6Timeout", ips)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("LookupIP waited %v; ipv6Timeout should still apply after A succeeds", elapsed)
	}
}

func TestLookupIPBothFail(t *testing.T) {
	r := testResolver(&staticDNSClient{}, 50*time.Millisecond)
	ips, err := r.LookupIP(context.Background(), "missing.example")
	if err != resolver.ErrIPNotFound {
		t.Fatalf("LookupIP err = %v, want ErrIPNotFound", err)
	}
	if len(ips) != 0 {
		t.Fatalf("LookupIP ips = %v, want empty", ips)
	}
}

func cachedAResolver(t testing.TB) (*Resolver, *D.Msg) {
	t.Helper()
	client := &staticDNSClient{a: []net.IP{net.ParseIP("192.0.2.1").To4()}}
	r := testResolver(client, 50*time.Millisecond)
	req := new(D.Msg)
	req.SetQuestion("warm.example.", D.TypeA)
	if _, err := r.ExchangeContext(context.Background(), req); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	return r, req
}

func TestCacheHitDoesNotMutateStoredMessage(t *testing.T) {
	r, req := cachedAResolver(t)
	msg, err := r.ExchangeContext(context.Background(), req)
	if err != nil || len(msg.Answer) != 1 {
		t.Fatalf("first hit: msg=%v err=%v", msg, err)
	}
	msg.Answer[0].Header().Ttl = 1
	msg.Id = 0x1234
	a, ok := msg.Answer[0].(*D.A)
	if !ok || len(a.A) == 0 {
		t.Fatalf("first hit answer type %T", msg.Answer[0])
	}
	origOctet := a.A[0]
	a.A[0] ^= 0xff

	msg2, err := r.ExchangeContext(context.Background(), req)
	if err != nil || len(msg2.Answer) != 1 {
		t.Fatalf("second hit: msg=%v err=%v", msg2, err)
	}
	if msg2.Answer[0].Header().Ttl == 1 {
		t.Fatal("cached RR TTL was mutated by the previous caller")
	}
	if msg2.Id == 0x1234 {
		t.Fatal("cached message header was mutated by the previous caller")
	}
	a2, ok := msg2.Answer[0].(*D.A)
	if !ok || len(a2.A) == 0 {
		t.Fatalf("second hit answer type %T", msg2.Answer[0])
	}
	if a2.A[0] != origOctet {
		t.Fatal("cached A rdata was mutated by the previous caller")
	}
}

func TestDNSNameKeyStripsOneTrailingDot(t *testing.T) {
	if got := dnsNameKey("Example.COM."); got != "example.com" {
		t.Fatalf("dnsNameKey(Example.COM.) = %q", got)
	}
	// TrimSuffix, not TrimRight: keep extra dots so DNAME owners stay distinct.
	if got := dnsNameKey("foo.bar.."); got != "foo.bar." {
		t.Fatalf("dnsNameKey(foo.bar..) = %q, want foo.bar.", got)
	}
}

func TestCacheSeparatesAAndAAAA(t *testing.T) {
	client := &staticDNSClient{
		a:    []net.IP{net.ParseIP("192.0.2.1").To4()},
		aaaa: []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)
	ctx := context.Background()
	v4, err := r.LookupIPv4(ctx, "dual.example")
	if err != nil || len(v4) != 1 || v4[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("LookupIPv4 = %v, %v", v4, err)
	}
	v6, err := r.LookupIPv6(ctx, "dual.example")
	if err != nil || len(v6) != 1 || v6[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("LookupIPv6 = %v, %v", v6, err)
	}
}

func extraHasOPT(rrs []D.RR) bool {
	for _, rr := range rrs {
		if rr != nil && rr.Header().Rrtype == D.TypeOPT {
			return true
		}
	}
	return false
}

func extraTailHasOPT(rrs []D.RR) bool {
	if rrs == nil {
		return false
	}
	tail := rrs[:cap(rrs)]
	for i := len(rrs); i < len(tail); i++ {
		if tail[i] != nil && tail[i].Header().Rrtype == D.TypeOPT {
			return true
		}
	}
	return false
}

func TestPutMsgToCacheStripsOPTKeepsCallerExtra(t *testing.T) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("example.com.", D.TypeA)
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{&D.A{
		Hdr: D.RR_Header{Name: "example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 300},
		A:   net.IPv4(192, 0, 2, 1).To4(),
	}}
	opt := &D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT, Class: 1232, Ttl: 0}}
	glue := &D.A{
		Hdr: D.RR_Header{Name: "ns.example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 300},
		A:   net.IPv4(192, 0, 2, 53).To4(),
	}
	resp.Extra = []D.RR{glue, opt}

	putMsgToCache(cache, req.Question[0], resp)

	if len(resp.Extra) != 2 || resp.Extra[0] != glue || resp.Extra[1] != opt {
		t.Fatalf("caller Extra mutated: %#v", resp.Extra)
	}

	stored, _, hit := cache.GetWithExpire(cacheKey(req.Question[0]))
	if !hit || stored == nil {
		t.Fatal("expected cache hit after EDNS put")
	}
	if extraHasOPT(stored.Extra) {
		t.Fatal("stored Extra still contains OPT")
	}
	if extraTailHasOPT(stored.Extra) {
		t.Fatal("stored Extra backing array retained discarded OPT")
	}
	if len(stored.Extra) != 1 {
		t.Fatalf("stored Extra = %d, want glue only", len(stored.Extra))
	}

	got, _, hit := getMsgFromCache(cache, req.Question[0])
	if !hit || got == nil || extraHasOPT(got.Extra) {
		t.Fatalf("GET Extra has OPT: %#v hit=%v", got, hit)
	}
	a, ok := got.Answer[0].(*D.A)
	if !ok || !a.A.Equal(net.IPv4(192, 0, 2, 1)) {
		t.Fatalf("GET A = %v", got.Answer)
	}
	a.A[0] ^= 0xff
	got2, _, _ := getMsgFromCache(cache, req.Question[0])
	a2 := got2.Answer[0].(*D.A)
	if a2.A[0] != 192 {
		t.Fatal("cached A rdata mutated after EDNS put/get")
	}
}

func TestPutMsgToCacheEDNSZeroFlagsStillCaches(t *testing.T) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("edns-zero.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{&D.A{
		Hdr: D.RR_Header{Name: "edns-zero.example.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
		A:   net.IPv4(192, 0, 2, 9).To4(),
	}}
	resp.Extra = []D.RR{&D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT, Class: 1232, Ttl: 0}}}

	putMsgToCache(cache, req.Question[0], resp)
	got, _, hit := getMsgFromCache(cache, req.Question[0])
	if !hit || got == nil || len(got.Answer) != 1 {
		t.Fatal("OPT.Hdr.Ttl=0 (EDNS flags) must not prevent caching a positive TTL")
	}
}

func TestPutMsgToCacheTTLZeroNotCached(t *testing.T) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("zero-ttl.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{&D.A{
		Hdr: D.RR_Header{Name: "zero-ttl.example.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 0},
		A:   net.IPv4(192, 0, 2, 8).To4(),
	}}
	putMsgToCache(cache, req.Question[0], resp)
	_, _, hit := getMsgFromCache(cache, req.Question[0])
	if hit {
		t.Fatal("ttl=0 answer must not be cached")
	}
}

func TestNegativeCacheNXDOMAINSOAIsolation(t *testing.T) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("missing.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetRcode(req, D.RcodeNameError)
	soa := &D.SOA{
		Hdr:     D.RR_Header{Name: "example.", Rrtype: D.TypeSOA, Class: D.ClassINET, Ttl: 30},
		Ns:      "ns.example.",
		Mbox:    "hostmaster.example.",
		Serial:  1,
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  30,
	}
	resp.Ns = []D.RR{soa}
	resp.Extra = []D.RR{&D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT, Class: 1232, Ttl: 0}}}

	putMsgToCache(cache, req.Question[0], resp)

	got, _, hit := getMsgFromCache(cache, req.Question[0])
	if !hit || got == nil {
		t.Fatal("NXDOMAIN with SOA should be cached")
	}
	if got.Rcode != D.RcodeNameError {
		t.Fatalf("Rcode = %d, want NXDOMAIN", got.Rcode)
	}
	if extraHasOPT(got.Extra) {
		t.Fatal("NXDOMAIN GET Extra still has OPT")
	}
	gotSOA, ok := got.Ns[0].(*D.SOA)
	if !ok {
		t.Fatalf("Ns[0] type %T", got.Ns[0])
	}
	gotSOA.Serial = 99
	gotSOA.Hdr.Ttl = 1
	gotSOA.Ns = "mutated.example."

	got2, _, hit := getMsgFromCache(cache, req.Question[0])
	if !hit {
		t.Fatal("second NXDOMAIN GET missed")
	}
	soa2 := got2.Ns[0].(*D.SOA)
	if soa2.Serial != 1 || soa2.Hdr.Ttl == 1 || soa2.Ns != "ns.example." {
		t.Fatalf("cached SOA mutated: %+v", soa2)
	}
	if soa.Serial != 1 || soa.Ns != "ns.example." {
		t.Fatal("caller SOA mutated by cache put")
	}
}

func TestNegativeCacheNXDOMAINWithoutSOANotCached(t *testing.T) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("empty-nx.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetRcode(req, D.RcodeNameError)
	resp.Extra = []D.RR{&D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT, Class: 1232, Ttl: 0}}}
	putMsgToCache(cache, req.Question[0], resp)
	_, _, hit := getMsgFromCache(cache, req.Question[0])
	if hit {
		t.Fatal("NXDOMAIN without SOA/records must not be cached (ttl=0)")
	}
}

type countingDNSClient struct {
	mu    sync.Mutex
	calls int
	a     []net.IP
	aaaa  []net.IP
}

func (c *countingDNSClient) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	q := m.Question[0]
	msg := new(D.Msg)
	msg.SetReply(m)
	switch q.Qtype {
	case D.TypeA:
		for _, ip := range c.a {
			msg.Answer = append(msg.Answer, &D.A{
				Hdr: D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
				A:   ip,
			})
		}
	case D.TypeAAAA:
		for _, ip := range c.aaaa {
			msg.Answer = append(msg.Answer, &D.AAAA{
				Hdr:  D.RR_Header{Name: q.Name, Rrtype: D.TypeAAAA, Class: D.ClassINET, Ttl: 60},
				AAAA: ip,
			})
		}
	default:
		msg.SetRcode(m, D.RcodeNameError)
	}
	return msg, nil
}

func (c *countingDNSClient) Address() string { return "counting" }

func (c *countingDNSClient) ResetConnection() {}

func (c *countingDNSClient) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitCalls(t *testing.T, c *countingDNSClient, min int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Calls() >= min {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client calls = %d, want >= %d", c.Calls(), min)
}

func TestLookupIPCacheHitDoesNotMutateStoredRdata(t *testing.T) {
	r, req := cachedAResolver(t)
	ips, err := r.LookupIPv4(context.Background(), "warm.example")
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("LookupIPv4 = %v, %v", ips, err)
	}

	msg, err := r.ExchangeContext(context.Background(), req)
	if err != nil || len(msg.Answer) != 1 {
		t.Fatalf("ExchangeContext after lookup: %v %v", msg, err)
	}
	a := msg.Answer[0].(*D.A)
	orig := a.A[0]
	a.A[0] ^= 0xff
	msg.Answer[0].Header().Ttl = 1

	msg2, err := r.ExchangeContext(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	a2 := msg2.Answer[0].(*D.A)
	if a2.A[0] != orig {
		t.Fatal("lookupIP peek mutated cached A octets")
	}
	if msg2.Answer[0].Header().Ttl == 1 {
		t.Fatal("lookupIP peek mutated cached TTL")
	}
}

func TestLookupIPv6CacheHitFresh(t *testing.T) {
	client := &countingDNSClient{aaaa: []net.IP{net.ParseIP("2001:db8::1")}}
	r := testResolver(client, 50*time.Millisecond)
	ctx := context.Background()
	got, err := r.LookupIPv6(ctx, "v6.example")
	if err != nil || len(got) != 1 || got[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("warm LookupIPv6 = %v, %v", got, err)
	}
	calls := client.Calls()
	got, err = r.LookupIPv6(ctx, "v6.example")
	if err != nil || len(got) != 1 || got[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("hit LookupIPv6 = %v, %v", got, err)
	}
	if client.Calls() != calls {
		t.Fatalf("AAAA cache hit queried upstream (%d -> %d)", calls, client.Calls())
	}
}

func TestLookupIPNegativeCachedNXDOMAIN(t *testing.T) {
	r := testResolver(&staticDNSClient{}, 50*time.Millisecond)
	req := new(D.Msg)
	req.SetQuestion("nx.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetRcode(req, D.RcodeNameError)
	resp.Ns = []D.RR{&D.SOA{
		Hdr:    D.RR_Header{Name: "example.", Rrtype: D.TypeSOA, Class: D.ClassINET, Ttl: 30},
		Ns:     "ns.example.",
		Mbox:   "hostmaster.example.",
		Minttl: 30,
	}}
	putMsgToCache(r.cache, req.Question[0], resp)

	ips, err := r.LookupIPv4(context.Background(), "nx.example")
	if err != resolver.ErrIPNotFound {
		t.Fatalf("LookupIPv4 NXDOMAIN err = %v, want ErrIPNotFound", err)
	}
	if len(ips) != 0 {
		t.Fatalf("LookupIPv4 NXDOMAIN ips = %v", ips)
	}

	req6 := new(D.Msg)
	req6.SetQuestion("nx.example.", D.TypeAAAA)
	resp6 := resp.Copy()
	resp6.SetQuestion("nx.example.", D.TypeAAAA)
	resp6.SetRcode(req6, D.RcodeNameError)
	putMsgToCache(r.cache, req6.Question[0], resp6)
	ips, err = r.LookupIPv6(context.Background(), "nx.example")
	if err != resolver.ErrIPNotFound || len(ips) != 0 {
		t.Fatalf("LookupIPv6 NXDOMAIN = %v, %v", ips, err)
	}
}

func TestLookupIPStaleRefreshesWithoutWAN(t *testing.T) {
	client := &countingDNSClient{a: []net.IP{net.ParseIP("192.0.2.1").To4()}}
	r := testResolver(client, 50*time.Millisecond)
	ctx := context.Background()
	if _, err := r.LookupIPv4(ctx, "stale.example"); err != nil {
		t.Fatalf("warm: %v", err)
	}
	warmCalls := client.Calls()

	q := D.Question{Name: D.Fqdn("stale.example"), Qtype: D.TypeA, Qclass: D.ClassINET}
	stored, _, hit := r.cache.GetWithExpire(cacheKey(q))
	if !hit || stored == nil {
		t.Fatal("warm cache miss")
	}
	r.cache.SetWithExpire(cacheKey(q), stored, time.Now().Add(-time.Second))

	ips, err := r.LookupIPv4(ctx, "stale.example")
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("stale LookupIPv4 = %v, %v", ips, err)
	}
	waitCalls(t, client, warmCalls+1)
}

func TestLookupIPConcurrentCacheReplaceAndClear(t *testing.T) {
	client := &countingDNSClient{
		a:    []net.IP{net.ParseIP("192.0.2.1").To4()},
		aaaa: []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)
	ctx := context.Background()
	if _, err := r.LookupIPv4(ctx, "conc.example"); err != nil {
		t.Fatal(err)
	}

	req := new(D.Msg)
	req.SetQuestion("conc.example.", D.TypeA)
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{&D.A{
		Hdr: D.RR_Header{Name: "conc.example.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
		A:   net.IPv4(192, 0, 2, 1).To4(),
	}}

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n + 1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			putMsgToCache(r.cache, req.Question[0], resp)
			if i%3 == 0 {
				r.cache.Clear()
			}
		}
	}()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = r.LookupIPv4(ctx, "conc.example")
			_, _ = r.LookupIPv6(ctx, "conc.example")
		}()
	}
	wg.Wait()
}

func BenchmarkGetMsgFromCacheHit(b *testing.B) {
	cache := Config{}.newCache()
	req := new(D.Msg)
	req.SetQuestion("example.com.", D.TypeA)
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{&D.A{
		Hdr: D.RR_Header{Name: "example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 300},
		A:   net.IPv4(192, 0, 2, 1).To4(),
	}}
	putMsgToCache(cache, req.Question[0], resp)

	q := req.Question[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, _, hit := getMsgFromCache(cache, q)
		if !hit || msg == nil {
			b.Fatal("cache miss")
		}
	}
}

func BenchmarkExchangeContextCacheHit(b *testing.B) {
	r, req := cachedAResolver(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, err := r.ExchangeContext(ctx, req)
		if err != nil || msg == nil {
			b.Fatalf("ExchangeContext: %v", err)
		}
	}
}

func BenchmarkLookupIPv4CacheHit(b *testing.B) {
	r, _ := cachedAResolver(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ips, err := r.LookupIPv4(ctx, "warm.example")
		if err != nil || len(ips) != 1 {
			b.Fatalf("LookupIPv4: %v %v", ips, err)
		}
	}
}
