package geodata

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/geodata/router"

	"google.golang.org/protobuf/proto"
)

// geoSiteTestMu serializes tests that mutate process-wide loader/matcher caches.
var geoSiteTestMu sync.Mutex

type fakeGeoLoader struct {
	mu           sync.Mutex
	lists        map[string][]*router.Domain
	calls        map[string]int
	failLeft     map[string]int
	holdFirst    <-chan struct{}
	enteredFirst chan struct{}
	enteredOnce  sync.Once
}

func (f *fakeGeoLoader) ncalls(list string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[list]
}

func (f *fakeGeoLoader) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fakeGeoLoader) LoadSiteByPath(_, list string) ([]*router.Domain, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[list]++
	if n := f.failLeft[list]; n > 0 {
		f.failLeft[list] = n - 1
		f.mu.Unlock()
		return nil, errors.New("synthetic list load failure")
	}
	domains, ok := f.lists[list]
	hold := f.holdFirst
	entered := f.enteredFirst
	f.mu.Unlock()

	if entered != nil {
		f.enteredOnce.Do(func() { close(entered) })
	}
	if hold != nil {
		<-hold
	}
	if !ok {
		return nil, errors.New("list not found: " + list)
	}
	out := make([]*router.Domain, len(domains))
	copy(out, domains)
	return out, nil
}

func (f *fakeGeoLoader) LoadSiteByBytes([]byte, string) ([]*router.Domain, error) {
	return nil, errors.New("fakeGeoLoader does not support LoadSiteByBytes")
}

func (f *fakeGeoLoader) LoadIPByPath(string, string) ([]*router.CIDR, error) {
	return nil, errors.New("fakeGeoLoader does not support LoadIPByPath")
}

func (f *fakeGeoLoader) LoadIPByBytes([]byte, string) ([]*router.CIDR, error) {
	return nil, errors.New("fakeGeoLoader does not support LoadIPByBytes")
}

func setupFakeGeoSite(tb testing.TB, f *fakeGeoLoader) {
	tb.Helper()
	geoSiteTestMu.Lock()
	tb.Cleanup(func() {
		ClearGeoSiteCache()
		SetLoader("memconservative")
		SetSiteMatcher("succinct")
		geoSiteTestMu.Unlock()
	})
	RegisterGeoDataLoaderImplementationCreator("testfake", func() LoaderImplementation { return f })
	SetLoader("testfake")
	SetSiteMatcher("succinct")
	ClearGeoSiteCache()
}

func attrCN() []*router.Domain_Attribute {
	return []*router.Domain_Attribute{{
		Key:        "cn",
		TypedValue: &router.Domain_Attribute_BoolValue{BoolValue: true},
	}}
}

// syntheticCNDomains is a deterministic GeoSite "cn" list used by unit tests
// and the external A/B fixture. Mix of Full / Domain / Plain plus one
// attribute-tagged name so cn vs cn@cn diverge.
func syntheticCNDomains() []*router.Domain {
	return []*router.Domain{
		{Type: router.Domain_Full, Value: "full.example.com"},
		{Type: router.Domain_Domain, Value: "example.org"},
		{Type: router.Domain_Plain, Value: "tracker"},
		{Type: router.Domain_Full, Value: "cn-only.example", Attribute: attrCN()},
		{Type: router.Domain_Full, Value: "global.example"},
	}
}

func syntheticGeoSiteDat() []byte {
	b, err := proto.Marshal(&router.GeoSiteList{
		Entry: []*router.GeoSite{{
			CountryCode: "cn",
			Domain:      syntheticCNDomains(),
		}},
	})
	if err != nil {
		panic(err)
	}
	return b
}

func assertApply(t *testing.T, m router.DomainMatcher, host string, want bool) {
	t.Helper()
	if got := m.ApplyDomain(host); got != want {
		t.Fatalf("ApplyDomain(%q)=%v want %v", host, got, want)
	}
}

func TestLoadGeoSiteMatcherSequentialCacheHit(t *testing.T) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(t, f)

	m1, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := LoadGeoSiteMatcher("CN") // case-insensitive cache key
	if err != nil {
		t.Fatal(err)
	}
	if f.ncalls("cn") != 1 {
		t.Fatalf("sequential same matcher decoded %d times, want 1", f.ncalls("cn"))
	}
	if m1.Count() != m2.Count() {
		t.Fatalf("cached matcher count %d vs %d", m1.Count(), m2.Count())
	}
	assertApply(t, m2, "full.example.com", true)
	assertApply(t, m2, "www.example.org", true)
	assertApply(t, m2, "ads.tracker.net", true)
	assertApply(t, m2, "not-listed.test", false)
}

func TestLoadGeoSiteMatcherSequentialAttributeVariants(t *testing.T) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(t, f)

	all, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := LoadGeoSiteMatcher("cn@cn")
	if err != nil {
		t.Fatal(err)
	}
	// List SF does not retain proto slices, so the second variant re-decodes.
	if f.ncalls("cn") != 2 {
		t.Fatalf("sequential attribute variants decoded %d times, want 2 (extra decode)", f.ncalls("cn"))
	}
	assertApply(t, all, "global.example", true)
	assertApply(t, all, "cn-only.example", true)
	assertApply(t, filtered, "cn-only.example", true)
	assertApply(t, filtered, "global.example", false)
	assertApply(t, filtered, "full.example.com", false)
}

func TestLoadGeoSiteMatcherConcurrentSameListSharing(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{})
	f := &fakeGeoLoader{
		lists:        map[string][]*router.Domain{"cn": syntheticCNDomains()},
		holdFirst:    hold,
		enteredFirst: entered,
	}
	setupFakeGeoSite(t, f)

	var (
		wg      sync.WaitGroup
		errAll  atomic.Value
		errAttr atomic.Value
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := LoadGeoSiteMatcher("cn")
		if err != nil {
			errAll.Store(err)
		}
	}()
	<-entered // first decode is in-flight
	go func() {
		defer wg.Done()
		_, err := LoadGeoSiteMatcher("cn@cn")
		if err != nil {
			errAttr.Store(err)
		}
	}()
	// Let the second matcher enter list SF while the first decode is held.
	time.Sleep(50 * time.Millisecond)
	close(hold)
	wg.Wait()
	if err, _ := errAll.Load().(error); err != nil {
		t.Fatal(err)
	}
	if err, _ := errAttr.Load().(error); err != nil {
		t.Fatal(err)
	}
	if f.ncalls("cn") != 1 {
		t.Fatalf("concurrent same-list loads decoded %d times, want 1 in-flight share", f.ncalls("cn"))
	}
}

func TestLoadGeoSiteMatcherFailureThenRetry(t *testing.T) {
	f := &fakeGeoLoader{
		lists:    map[string][]*router.Domain{"cn": syntheticCNDomains()},
		failLeft: map[string]int{"cn": 1},
	}
	setupFakeGeoSite(t, f)

	if _, err := LoadGeoSiteMatcher("cn"); err == nil {
		t.Fatal("expected synthetic failure")
	}
	m, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if f.ncalls("cn") != 2 {
		t.Fatalf("fail then retry decoded %d times, want 2", f.ncalls("cn"))
	}
	assertApply(t, m, "full.example.com", true)
}

func TestLoadGeoSiteMatcherClearCache(t *testing.T) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(t, f)

	if _, err := LoadGeoSiteMatcher("cn"); err != nil {
		t.Fatal(err)
	}
	ClearGeoSiteCache()
	if _, err := LoadGeoSiteMatcher("cn"); err != nil {
		t.Fatal(err)
	}
	if f.ncalls("cn") != 2 {
		t.Fatalf("after ClearGeoSiteCache decoded %d times, want 2", f.ncalls("cn"))
	}
}

func TestLoadGeoSiteMatcherNotWrapper(t *testing.T) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(t, f)

	pos, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatal(err)
	}
	neg, err := LoadGeoSiteMatcher("!cn")
	if err != nil {
		t.Fatal(err)
	}
	if f.ncalls("cn") != 1 {
		t.Fatalf("!cn should reuse cached cn matcher, decoded %d times", f.ncalls("cn"))
	}
	assertApply(t, pos, "full.example.com", true)
	assertApply(t, neg, "full.example.com", false)
	assertApply(t, pos, "not-listed.test", false)
	assertApply(t, neg, "not-listed.test", true)
}

func TestLoadGeoSiteMatcherSuccinctAndMph(t *testing.T) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(t, f)

	succinct, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatal(err)
	}
	ClearGeoSiteCache()
	SetSiteMatcher("mph")
	mph, err := LoadGeoSiteMatcher("cn")
	if err != nil {
		t.Fatal(err)
	}
	hosts := []struct {
		host string
		want bool
	}{
		{"full.example.com", true},
		{"www.example.org", true},
		{"example.org", true},
		{"ads.tracker.net", true},
		{"cn-only.example", true},
		{"not-listed.test", false},
	}
	for _, h := range hosts {
		if succinct.ApplyDomain(h.host) != h.want {
			t.Fatalf("succinct ApplyDomain(%q)=%v want %v", h.host, !h.want, h.want)
		}
		if mph.ApplyDomain(h.host) != h.want {
			t.Fatalf("mph ApplyDomain(%q)=%v want %v", h.host, !h.want, h.want)
		}
	}
	if f.ncalls("cn") != 2 {
		t.Fatalf("succinct+mph decoded %d times, want 2 (cache cleared between)", f.ncalls("cn"))
	}
}

func TestSyntheticGeoSiteFixtureRoundTrip(t *testing.T) {
	raw := syntheticGeoSiteDat()
	if len(raw) == 0 {
		t.Fatal("empty fixture")
	}
	var list router.GeoSiteList
	if err := proto.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Entry) != 1 || list.Entry[0].GetCountryCode() != "cn" {
		t.Fatalf("fixture entries = %+v", list.Entry)
	}
	if got, want := len(list.Entry[0].Domain), len(syntheticCNDomains()); got != want {
		t.Fatalf("fixture domains %d want %d", got, want)
	}
}

func TestWriteSyntheticGeoSiteFixture(t *testing.T) {
	dir := os.Getenv("L18_FIXTURE_DIR")
	if dir == "" {
		t.Skip("set L18_FIXTURE_DIR to write L18-synthetic-geosite.dat")
	}
	path := filepath.Join(dir, "L18-synthetic-geosite.dat")
	if err := os.WriteFile(path, syntheticGeoSiteDat(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// BenchmarkGeoSiteSequentialAttributeVariants documents the extra decode
// after list SF no longer stores proto slices: each cn + cn@cn pair loads
// the raw list twice. Matcher-cache hits for the same matcherName stay at
// one decode (see BenchmarkGeoSiteSameMatcherCacheHit).
func BenchmarkGeoSiteSequentialAttributeVariants(b *testing.B) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(b, f)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClearGeoSiteCache()
		if _, err := LoadGeoSiteMatcher("cn"); err != nil {
			b.Fatal(err)
		}
		if _, err := LoadGeoSiteMatcher("cn@cn"); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if f.ncalls("cn") != 2*b.N {
		b.Fatalf("decoded %d times, want %d (2 per iteration)", f.ncalls("cn"), 2*b.N)
	}
}

func BenchmarkGeoSiteSameMatcherCacheHit(b *testing.B) {
	f := &fakeGeoLoader{lists: map[string][]*router.Domain{"cn": syntheticCNDomains()}}
	setupFakeGeoSite(b, f)
	if _, err := LoadGeoSiteMatcher("cn"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadGeoSiteMatcher("cn"); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if f.ncalls("cn") != 1 {
		b.Fatalf("cache hit path decoded %d times, want 1", f.ncalls("cn"))
	}
}
