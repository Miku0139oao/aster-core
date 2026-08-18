package kerneldirect

import (
	"io"
	"net/netip"
	"sort"
	"strings"
	"sync"
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

// Sink replaces the dynamic kernel DIRECT and PROXY decision sets.
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
	mu          sync.Mutex
	classifier  Classifier
	sink        Sink
	records     map[netip.Addr]*addressRecords
	maxEntries  uint32
	maxRecords  uint64
	recordsLen  uint64
	sequence    uint64
	evictions   uint64
	lastDirect  []netip.Prefix
	lastProxy   []netip.Prefix
	generation  uint64
	closed      bool
	stop        chan struct{}
	done        chan struct{}
	publishReqs chan publishRequest
	closeOnce   sync.Once
}

var registry = struct {
	sync.RWMutex
	controllers map[*controller]struct{}
}{controllers: make(map[*controller]struct{})}

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
		publishReqs: make(chan publishRequest, 1),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	registry.Lock()
	registry.controllers[c] = struct{}{}
	registry.Unlock()
	go c.expireLoop()
	go c.publishLoop()
	return c
}

// Statuses returns a control-plane snapshot of all active consumers.
// It holds each controller's mutex and scans its records, so it is not cheap.
func Statuses() []ControllerStatus {
	registry.RLock()
	controllers := make([]*controller, 0, len(registry.controllers))
	for c := range registry.controllers {
		controllers = append(controllers, c)
	}
	registry.RUnlock()
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
	registry.RLock()
	controllers := make([]*controller, 0, len(registry.controllers))
	for c := range registry.controllers {
		controllers = append(controllers, c)
	}
	registry.RUnlock()
	for _, c := range controllers {
		c.observe(host, answers)
	}
}

// Flush removes all learned addresses. It is called whenever routing state may
// have changed, so stale DIRECT decisions can never survive a reload.
func Flush() {
	registry.RLock()
	controllers := make([]*controller, 0, len(registry.controllers))
	for c := range registry.controllers {
		controllers = append(controllers, c)
	}
	registry.RUnlock()
	for _, c := range controllers {
		c.flush()
	}
}

func (c *controller) observe(host string, answers []DNSAnswer) {
	now := time.Now()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.removeExpiredLocked(now)
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
		isDirect := c.classifier(host, addr)
		address := c.records[addr]
		hostExists := false
		if address != nil {
			_, hostExists = address.byHost[host]
		}
		if !hostExists && c.recordsLen >= c.maxRecords {
			if address != nil {
				if isDirect {
					// Keep the existing decision rather than dropping a prior
					// PROXY observation to make room for another DIRECT host.
					continue
				}
				// A new PROXY host must be recorded, but still-valid PROXY
				// observations stay. Drop DIRECT hosts on this address first.
				c.dropDirectHostsLocked(address)
				if c.recordsLen >= c.maxRecords {
					c.dropOldestHostLocked(address)
				}
			} else {
				c.evictOldestLocked()
			}
		}
		if address == nil {
			if uint32(len(c.records)) >= c.maxEntries {
				c.evictOldestLocked()
			}
			address = &addressRecords{byHost: make(map[string]record)}
			c.records[addr] = address
		}
		c.touchLocked(address)
		if _, exists := address.byHost[host]; !exists {
			c.recordsLen++
		}
		address.byHost[host] = record{direct: isDirect, expires: now.Add(ttl)}
	}
	sets, generation, changed := c.changedSetsLocked()
	c.mu.Unlock()
	if changed {
		<-c.publish(sets, generation)
	}
}

func isUnsafeAddress(addr netip.Addr) bool {
	if !addr.IsValid() || resolver.IsFakeIP(addr) {
		return true
	}
	return !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()
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
	changed := false
	for addr, address := range c.records {
		for host, item := range address.byHost {
			if !item.expires.After(now) {
				delete(address.byHost, host)
				c.recordsLen--
				changed = true
			}
		}
		if len(address.byHost) == 0 {
			delete(c.records, addr)
		}
	}
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
			c.recordsLen--
		}
	}
}

func (c *controller) dropOldestHostLocked(address *addressRecords) {
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
	if found {
		delete(address.byHost, oldestHost)
		c.recordsLen--
	}
}

func (c *controller) evictOldestLocked() {
	var oldestAddr netip.Addr
	var oldestSequence uint64
	for addr, address := range c.records {
		if !oldestAddr.IsValid() || address.lastSeen < oldestSequence {
			oldestAddr = addr
			oldestSequence = address.lastSeen
		}
	}
	if oldestAddr.IsValid() {
		c.recordsLen -= uint64(len(c.records[oldestAddr].byHost))
		delete(c.records, oldestAddr)
		c.evictions++
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
	sets DecisionSets
	done chan struct{}
}

func (c *controller) publishLoop() {
	for {
		select {
		case req := <-c.publishReqs:
			c.sink(req.sets)
			close(req.done)
		case <-c.stop:
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
	case c.publishReqs <- publishRequest{sets: sets, done: done}:
	}
	return done
}

func (c *controller) flush() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.records = make(map[netip.Addr]*addressRecords)
	c.recordsLen = 0
	changed := len(c.lastDirect) != 0 || len(c.lastProxy) != 0
	c.lastDirect = nil
	c.lastProxy = nil
	if changed {
		c.generation++
	}
	generation := c.generation
	c.mu.Unlock()
	if changed {
		<-c.publish(DecisionSets{Direct: &netipx.IPSet{}, Proxy: &netipx.IPSet{}}, generation)
	}
}

func (c *controller) Close() error {
	c.closeOnce.Do(func() {
		registry.Lock()
		delete(registry.controllers, c)
		registry.Unlock()
		c.mu.Lock()
		c.closed = true
		c.generation++
		c.mu.Unlock()
		close(c.stop)
		<-c.done
	})
	return nil
}
