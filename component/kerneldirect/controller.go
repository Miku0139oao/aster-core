package kerneldirect

import (
	"io"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/component/resolver"

	"go4.org/netipx"
	"golang.org/x/exp/slices"
)

const (
	minimumTTL        = time.Second
	maximumTTL        = time.Hour
	DefaultMaxEntries = uint32(4096)
	MaximumMaxEntries = uint32(65536)
)

// DNSAnswer is a real address returned by Aster's DNS service.
type DNSAnswer struct {
	Addr netip.Addr
	TTL  time.Duration
}

// Classifier returns true only when the destination can safely bypass Aster.
type Classifier func(host string, addr netip.Addr) bool

// Sink replaces the dynamic kernel DIRECT and PROXY decision sets. It must
// return and must not synchronously call ObserveDNS, ObserveFlow, or Flush;
// publication is deliberately serialized and those mutating calls wait for the
// current generation to finish. Read-only Statuses and asynchronous mutation
// are safe.
type Sink func(DecisionSets)

type record struct {
	direct  bool
	expires time.Time
}

type addressRecords struct {
	byHost   map[string]record
	lastSeen uint64
}

type ControllerOptions struct {
	MaxEntries uint32
}

type ControllerStatus struct {
	MaxEntries       uint32 `json:"max_entries"`
	MaxRecords       uint32 `json:"max_records"`
	LearnedAddresses int    `json:"learned_addresses"`
	DirectAddresses  int    `json:"direct_addresses"`
	ProxyAddresses   int    `json:"proxy_addresses"`
	LearnedDomains   int    `json:"learned_domains"`
	Evictions        uint64 `json:"evictions"`
}

type controller struct {
	mu           sync.Mutex
	classifierMu sync.Mutex
	classifier   Classifier
	sink         Sink
	records      map[netip.Addr]*addressRecords
	maxEntries   uint32
	maxRecords   uint64
	recordsLen   uint64
	sequence     uint64
	evictions    uint64
	lastDirect   []netip.Prefix
	lastProxy    []netip.Prefix
	nextExpiry   time.Time
	generation   uint64
	applied      uint64
	flushSeq     uint64
	applyWaiters map[uint64]chan struct{}
	closed       bool
	stop         chan struct{}
	done         chan struct{}
	publishReqs  chan publishRequest
	closeOnce    sync.Once
}

var activeControllers atomic.Int64

var registry = struct {
	sync.RWMutex
	controllers map[*controller]struct{}
}{controllers: make(map[*controller]struct{})}

// liveControllers is a copy-on-write snapshot so ObserveDNS/ObserveFlow/Flush
// do not allocate or take registry.RLock on the per-flow refresh path.
var liveControllers atomic.Pointer[[]*controller]

func liveControllerList() []*controller {
	list := liveControllers.Load()
	if list == nil {
		return nil
	}
	return *list
}

func publishLiveControllersLocked() {
	list := make([]*controller, 0, len(registry.controllers))
	for c := range registry.controllers {
		list = append(list, c)
	}
	liveControllers.Store(&list)
}

// Register installs one kernel DIRECT consumer. The returned closer unregisters
// it and stops its expiry worker.
func Register(classifier Classifier, sink Sink, options ...ControllerOptions) io.Closer {
	maxEntries := DefaultMaxEntries
	if len(options) > 0 && options[0].MaxEntries != 0 {
		maxEntries = options[0].MaxEntries
	}
	if maxEntries > MaximumMaxEntries {
		maxEntries = MaximumMaxEntries
	}
	c := &controller{
		classifier:  classifier,
		sink:        sink,
		records:     make(map[netip.Addr]*addressRecords),
		maxEntries:  maxEntries,
		maxRecords:  uint64(maxEntries) * 4,
		publishReqs: make(chan publishRequest),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	registry.Lock()
	registry.controllers[c] = struct{}{}
	publishLiveControllersLocked()
	registry.Unlock()
	activeControllers.Add(1)
	go c.expireLoop()
	go c.publishLoop()
	return c
}

// HasConsumers reports whether DNS observation work can affect an active
// controller. A false-negative during concurrent registration is fail-closed:
// that single response simply remains in userspace.
func HasConsumers() bool {
	return activeControllers.Load() > 0
}

// Statuses returns a control-plane snapshot of all active consumers.
// It holds each controller's mutex and scans its records, so it is not cheap.
func Statuses() []ControllerStatus {
	controllers := liveControllerList()
	statuses := make([]ControllerStatus, 0, len(controllers))
	for _, c := range controllers {
		if s := c.status(); s.MaxEntries != 0 {
			statuses = append(statuses, s)
		}
	}
	return statuses
}

// ObserveFlow records one live DIRECT/PROXY destination, including dests that
// never went through Aster DNS (pure-IP reconnects, lobby-issued battle IPs).
func ObserveFlow(host string, addr netip.Addr, ttl time.Duration) {
	addr = addr.Unmap()
	if isUnsafeAddress(addr) {
		return
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if host == "" {
		host = addr.String()
	}
	ObserveDNS(host, []DNSAnswer{{Addr: addr, TTL: ttl}})
}

// ObserveDNS sends one DNS response to all active kernel DIRECT consumers.
func ObserveDNS(host string, answers []DNSAnswer) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || len(answers) == 0 {
		return
	}
	for _, c := range liveControllerList() {
		c.observe(host, answers)
	}
}

// Flush removes all learned addresses. It is called whenever routing state may
// have changed, so stale DIRECT decisions can never survive a reload.
func Flush() {
	for _, c := range liveControllerList() {
		c.flush()
	}
}

func (c *controller) observe(host string, answers []DNSAnswer) {
	// Unexpired (host, addr) hits reuse the cached DIRECT/PROXY bit, touch LRU,
	// and extend TTL. Routing changes must Flush; reclassifying here would take
	// classifierMu and waitApplied on every live flow refresh.
	if c.tryRefreshExisting(host, answers) {
		return
	}
	// Classifier used to run under c.mu. Preserve its serialized and
	// quiescent-on-Close contract without holding the state mutex across a
	// potentially expensive routing callback.
	c.classifierMu.Lock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.classifierMu.Unlock()
		return
	}
	c.mu.Unlock()
	observedAt := time.Now()
	flushSeq := atomic.LoadUint64(&c.flushSeq)
	type classified struct {
		addr    netip.Addr
		expires time.Time
		direct  bool
	}
	var inlineItems [4]classified
	items := inlineItems[:0]
	if len(answers) > len(inlineItems) {
		items = make([]classified, 0, len(answers))
	}
	for _, answer := range answers {
		addr := answer.Addr.Unmap()
		if isUnsafeAddress(addr) {
			continue
		}
		ttl := answer.TTL
		if ttl < minimumTTL {
			ttl = minimumTTL
		} else if ttl > maximumTTL {
			ttl = maximumTTL
		}
		direct := false
		if c.classifier != nil {
			direct = c.classifier(host, addr)
		}
		items = append(items, classified{addr: addr, expires: observedAt.Add(ttl), direct: direct})
	}
	c.classifierMu.Unlock()

	c.mu.Lock()
	if c.closed || atomic.LoadUint64(&c.flushSeq) != flushSeq {
		c.mu.Unlock()
		return
	}
	now := time.Now()
	dirty := c.removeExpiredLocked(now)
	for _, item := range items {
		if !item.expires.After(now) {
			continue
		}
		address := c.records[item.addr]
		hostExists := false
		if address != nil {
			_, hostExists = address.byHost[host]
		}
		if !hostExists && c.recordsLen >= c.maxRecords {
			if address != nil {
				if item.direct {
					// Keep the existing decision rather than dropping a prior
					// PROXY observation to make room for another DIRECT host.
					continue
				}
				// A new PROXY host must be recorded, but still-valid PROXY
				// observations stay. Drop DIRECT hosts on this address first.
				before := c.recordsLen
				c.dropDirectHostsLocked(address)
				if len(address.byHost) == 0 {
					delete(c.records, item.addr)
					address = nil
				}
				if c.recordsLen != before {
					dirty = true
				}
				if address != nil && c.recordsLen >= c.maxRecords {
					if !c.dropOldestHostLocked(address, item.expires) {
						continue
					}
					dirty = true
				}
			}
			if address == nil && c.recordsLen >= c.maxRecords {
				if !c.evictOldestLocked() {
					c.healRecordsLenLocked()
					if c.recordsLen >= c.maxRecords {
						continue
					}
				} else {
					dirty = true
				}
			}
		}
		if address == nil {
			if uint32(len(c.records)) >= c.maxEntries {
				if !c.evictOldestLocked() {
					continue
				}
				dirty = true
			}
			if uint32(len(c.records)) >= c.maxEntries {
				continue
			}
			address = &addressRecords{byHost: make(map[string]record)}
			c.records[item.addr] = address
		}
		c.touchLocked(address)
		if prev, exists := address.byHost[host]; !exists {
			c.recordsLen++
			dirty = true
		} else if prev.direct != item.direct {
			dirty = true
		}
		address.byHost[host] = record{direct: item.direct, expires: item.expires}
		if c.nextExpiry.IsZero() || item.expires.Before(c.nextExpiry) {
			c.nextExpiry = item.expires
		}
	}
	var sets DecisionSets
	var generation uint64
	var changed bool
	if dirty {
		sets, generation, changed = c.changedSetsLocked()
	} else {
		generation = c.generation
	}
	c.mu.Unlock()
	c.commitPublish(sets, generation, changed)
}

func (c *controller) tryRefreshExisting(host string, answers []DNSAnswer) bool {
	c.mu.Lock()
	handled, wait := c.refreshExistingLocked(host, answers, time.Now())
	c.mu.Unlock()
	if handled && wait != nil {
		c.waitStop(wait)
	}
	return handled
}

func (c *controller) refreshExistingLocked(host string, answers []DNSAnswer, now time.Time) (handled bool, wait <-chan struct{}) {
	if c.closed {
		return true, nil
	}
	if c.nextExpiry.IsZero() || !now.Before(c.nextExpiry) {
		return false, nil
	}
	safe := 0
	for _, answer := range answers {
		addr := answer.Addr.Unmap()
		if isUnsafeAddress(addr) {
			continue
		}
		address := c.records[addr]
		if address == nil {
			return false, nil
		}
		prev, exists := address.byHost[host]
		if !exists || !prev.expires.After(now) {
			return false, nil
		}
		safe++
	}
	if safe == 0 {
		return false, nil
	}
	for _, answer := range answers {
		addr := answer.Addr.Unmap()
		if isUnsafeAddress(addr) {
			continue
		}
		ttl := answer.TTL
		if ttl < minimumTTL {
			ttl = minimumTTL
		} else if ttl > maximumTTL {
			ttl = maximumTTL
		}
		expires := now.Add(ttl)
		address := c.records[addr]
		prev := address.byHost[host]
		address.byHost[host] = record{direct: prev.direct, expires: expires}
		c.touchLocked(address)
		if c.nextExpiry.IsZero() || expires.Before(c.nextExpiry) {
			c.nextExpiry = expires
		}
	}
	generation := c.generation
	if generation <= atomic.LoadUint64(&c.applied) {
		return true, nil
	}
	done := c.applyWaiters[generation]
	if done == nil {
		if c.applyWaiters == nil {
			c.applyWaiters = make(map[uint64]chan struct{})
		}
		done = make(chan struct{})
		c.applyWaiters[generation] = done
	}
	return true, done
}

var (
	// Keep these aligned with the TC classifier's fail-closed skips so the
	// nftables learned exclude cannot hijack fake-IP, CGNAT, or this-network.
	ipv4ThisNetwork     = netip.MustParsePrefix("0.0.0.0/8")
	ipv4SharedNet       = netip.MustParsePrefix("100.64.0.0/10")
	ipv4BenchmarkingNet = netip.MustParsePrefix("198.18.0.0/15")
)

func isUnsafeAddress(addr netip.Addr) bool {
	if !addr.IsValid() || resolver.IsFakeIP(addr) {
		return true
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	return addr.Is4() && (ipv4ThisNetwork.Contains(addr) || ipv4SharedNet.Contains(addr) || ipv4BenchmarkingNet.Contains(addr))
}

func (c *controller) expireLoop() {
	ticker := time.NewTicker(time.Second)
	defer func() {
		ticker.Stop()
		close(c.done)
	}()
	for {
		select {
		case now := <-ticker.C:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			changed := c.removeExpiredLocked(now)
			var sets DecisionSets
			var generation uint64
			if changed {
				sets, generation, changed = c.changedSetsLocked()
			}
			c.mu.Unlock()
			if changed {
				c.publish(sets, generation)
			}
		case <-c.stop:
			return
		}
	}
}

func (c *controller) removeExpiredLocked(now time.Time) bool {
	if len(c.records) == 0 {
		c.nextExpiry = time.Time{}
		return false
	}
	if !c.nextExpiry.IsZero() && now.Before(c.nextExpiry) {
		return false
	}

	changed := false
	nextExpiry := time.Time{}
	for addr, address := range c.records {
		for host, item := range address.byHost {
			if !item.expires.After(now) {
				delete(address.byHost, host)
				c.decRecordsLenLocked(1)
				changed = true
				continue
			}
			if nextExpiry.IsZero() || item.expires.Before(nextExpiry) {
				nextExpiry = item.expires
			}
		}
		if len(address.byHost) == 0 {
			delete(c.records, addr)
		}
	}
	c.nextExpiry = nextExpiry
	return changed
}

func (c *controller) touchLocked(address *addressRecords) {
	if c.sequence == ^uint64(0) {
		ranked := make([]netip.Addr, 0, len(c.records))
		for addr := range c.records {
			ranked = append(ranked, addr)
		}
		sort.Slice(ranked, func(i, j int) bool {
			return c.records[ranked[i]].lastSeen < c.records[ranked[j]].lastSeen
		})
		for index, addr := range ranked {
			c.records[addr].lastSeen = uint64(index + 1)
		}
		c.sequence = uint64(len(ranked))
	}
	c.sequence++
	address.lastSeen = c.sequence
}

func (c *controller) dropDirectHostsLocked(address *addressRecords) {
	for host, item := range address.byHost {
		if item.direct {
			delete(address.byHost, host)
			c.decRecordsLenLocked(1)
		}
	}
}

func (c *controller) dropOldestHostLocked(address *addressRecords, incomingExpires time.Time) bool {
	var oldestHost string
	var oldestExpires time.Time
	found := false
	for host, item := range address.byHost {
		if !found || item.expires.Before(oldestExpires) {
			oldestHost = host
			oldestExpires = item.expires
			found = true
		}
	}
	if !found {
		return false
	}
	// Keep a longer-lived PROXY record instead of replacing it with a
	// shorter incoming observation that would forget the address early.
	if oldestExpires.After(incomingExpires) {
		return false
	}
	delete(address.byHost, oldestHost)
	c.decRecordsLenLocked(1)
	return true
}

func (c *controller) evictOldestLocked() bool {
	var oldestAddr netip.Addr
	var oldestSequence uint64
	for addr, address := range c.records {
		if !oldestAddr.IsValid() || address.lastSeen < oldestSequence {
			oldestAddr = addr
			oldestSequence = address.lastSeen
		}
	}
	if !oldestAddr.IsValid() {
		c.healRecordsLenLocked()
		return false
	}
	c.decRecordsLenLocked(uint64(len(c.records[oldestAddr].byHost)))
	delete(c.records, oldestAddr)
	c.evictions++
	return true
}

func (c *controller) decRecordsLenLocked(n uint64) {
	if n == 0 {
		return
	}
	if c.recordsLen >= n {
		c.recordsLen -= n
		return
	}
	c.recordsLen = 0
}

func (c *controller) healRecordsLenLocked() {
	if len(c.records) == 0 {
		c.recordsLen = 0
	}
}

func (c *controller) buildSetsLocked() DecisionSets {
	var directBuilder netipx.IPSetBuilder
	var proxyBuilder netipx.IPSetBuilder
	for addr, address := range c.records {
		direct := false
		proxy := false
		for _, item := range address.byHost {
			if item.direct {
				direct = true
			} else {
				proxy = true
				break
			}
		}
		// Proxy wins when multiple domains share an address.
		if proxy {
			proxyBuilder.Add(addr)
		} else if direct {
			directBuilder.Add(addr)
		}
	}
	direct, directErr := directBuilder.IPSet()
	if directErr != nil {
		direct = &netipx.IPSet{}
	}
	proxy, proxyErr := proxyBuilder.IPSet()
	if proxyErr != nil {
		proxy = &netipx.IPSet{}
	}
	return DecisionSets{Direct: direct, Proxy: proxy}
}

func (c *controller) status() ControllerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ControllerStatus{}
	}
	status := ControllerStatus{MaxEntries: c.maxEntries, MaxRecords: uint32(c.maxRecords), LearnedAddresses: len(c.records), LearnedDomains: int(c.recordsLen), Evictions: c.evictions}
	for _, address := range c.records {
		proxy := false
		direct := false
		for _, item := range address.byHost {
			if item.direct {
				direct = true
			} else {
				proxy = true
				break
			}
		}
		if proxy {
			status.ProxyAddresses++
		} else if direct {
			status.DirectAddresses++
		}
	}
	return status
}

func (c *controller) changedSetsLocked() (DecisionSets, uint64, bool) {
	sets := c.buildSetsLocked()
	direct := sets.Direct.Prefixes()
	proxy := sets.Proxy.Prefixes()
	if slices.Equal(direct, c.lastDirect) && slices.Equal(proxy, c.lastProxy) {
		return DecisionSets{}, c.generation, false
	}
	c.lastDirect = append(c.lastDirect[:0], direct...)
	c.lastProxy = append(c.lastProxy[:0], proxy...)
	c.generation++
	return sets, c.generation, true
}

type publishRequest struct {
	sets       DecisionSets
	generation uint64
	done       chan struct{}
}

func (c *controller) publishLoop() {
	for {
		select {
		case req := <-c.publishReqs:
			c.applyPublish(req)
		case <-c.stop:
			c.drainPublishReqs()
			return
		}
	}
}

func (c *controller) applyPublish(req publishRequest) {
	c.mu.Lock()
	apply := !c.closed && req.generation == c.generation
	c.mu.Unlock()
	if apply {
		if c.sink != nil {
			c.sink(req.sets)
		}
		c.mu.Lock()
		if !c.closed && req.generation == c.generation {
			atomic.StoreUint64(&c.applied, req.generation)
			c.releaseWaitersLocked()
		}
		c.mu.Unlock()
	}
	close(req.done)
}

func (c *controller) releaseWaitersLocked() {
	applied := atomic.LoadUint64(&c.applied)
	for generation, done := range c.applyWaiters {
		if generation <= applied || c.closed {
			close(done)
			delete(c.applyWaiters, generation)
		}
	}
	if len(c.applyWaiters) == 0 {
		c.applyWaiters = nil
	}
}

func (c *controller) waitStop(done <-chan struct{}) {
	select {
	case <-done:
	case <-c.stop:
	}
}

func (c *controller) waitApplied(generation uint64) {
	if generation <= atomic.LoadUint64(&c.applied) {
		return
	}
	c.mu.Lock()
	if c.closed || generation <= atomic.LoadUint64(&c.applied) {
		c.mu.Unlock()
		return
	}
	done := c.applyWaiters[generation]
	if done == nil {
		if c.applyWaiters == nil {
			c.applyWaiters = make(map[uint64]chan struct{})
		}
		done = make(chan struct{})
		c.applyWaiters[generation] = done
	}
	c.mu.Unlock()
	c.waitStop(done)
}

func (c *controller) commitPublish(sets DecisionSets, generation uint64, changed bool) {
	if changed {
		c.waitStop(c.publish(sets, generation))
	}
	c.waitApplied(generation)
}

func (c *controller) drainPublishReqs() {
	for {
		select {
		case req := <-c.publishReqs:
			close(req.done)
		default:
			return
		}
	}
}

func (c *controller) publish(sets DecisionSets, generation uint64) <-chan struct{} {
	done := make(chan struct{})
	c.mu.Lock()
	if c.closed || generation != c.generation {
		c.mu.Unlock()
		close(done)
		return done
	}
	c.mu.Unlock()
	select {
	case <-c.stop:
		close(done)
	case c.publishReqs <- publishRequest{sets: sets, generation: generation, done: done}:
	}
	return done
}

func (c *controller) flush() {
	atomic.AddUint64(&c.flushSeq, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.records = make(map[netip.Addr]*addressRecords)
	c.recordsLen = 0
	c.nextExpiry = time.Time{}
	changed := len(c.lastDirect) != 0 || len(c.lastProxy) != 0
	c.lastDirect = nil
	c.lastProxy = nil
	if changed {
		c.generation++
	}
	generation := c.generation
	c.mu.Unlock()
	c.commitPublish(DecisionSets{Direct: &netipx.IPSet{}, Proxy: &netipx.IPSet{}}, generation, changed)
}

func (c *controller) Close() error {
	c.closeOnce.Do(func() {
		registry.Lock()
		delete(registry.controllers, c)
		publishLiveControllersLocked()
		registry.Unlock()
		activeControllers.Add(-1)
		atomic.AddUint64(&c.flushSeq, 1)
		c.classifierMu.Lock()
		c.mu.Lock()
		c.closed = true
		c.generation++
		waiters := c.applyWaiters
		c.applyWaiters = nil
		c.mu.Unlock()
		c.classifierMu.Unlock()
		close(c.stop)
		for _, done := range waiters {
			close(done)
		}
		<-c.done
	})
	return nil
}
