package kerneldirect

import (
	"io"
	"net/netip"
	"strings"
	"sync"
	"time"

	"go4.org/netipx"
	"golang.org/x/exp/slices"
)

const (
	minimumTTL = time.Second
	maximumTTL = time.Hour
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

type controller struct {
	mu         sync.Mutex
	publishMu  sync.Mutex
	classifier Classifier
	sink       Sink
	records    map[netip.Addr]map[string]record
	lastDirect []netip.Prefix
	lastProxy  []netip.Prefix
	generation uint64
	closed     bool
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

var registry = struct {
	sync.RWMutex
	controllers map[*controller]struct{}
}{controllers: make(map[*controller]struct{})}

// Register installs one kernel DIRECT consumer. The returned closer unregisters
// it and stops its expiry worker.
func Register(classifier Classifier, sink Sink) io.Closer {
	c := &controller{
		classifier: classifier,
		sink:       sink,
		records:    make(map[netip.Addr]map[string]record),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	registry.Lock()
	registry.controllers[c] = struct{}{}
	registry.Unlock()
	go c.expireLoop()
	return c
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
	for _, answer := range answers {
		addr := answer.Addr.Unmap()
		if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
			continue
		}
		ttl := answer.TTL
		if ttl < minimumTTL {
			ttl = minimumTTL
		} else if ttl > maximumTTL {
			ttl = maximumTTL
		}
		byHost := c.records[addr]
		if byHost == nil {
			byHost = make(map[string]record)
			c.records[addr] = byHost
		}
		byHost[host] = record{
			direct:  c.classifier(host, addr),
			expires: now.Add(ttl),
		}
	}
	c.removeExpiredLocked(now)
	sets, generation, changed := c.changedSetsLocked()
	c.mu.Unlock()
	if changed {
		c.publish(sets, generation)
	}
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
	for addr, byHost := range c.records {
		for host, item := range byHost {
			if !item.expires.After(now) {
				delete(byHost, host)
				changed = true
			}
		}
		if len(byHost) == 0 {
			delete(c.records, addr)
		}
	}
	return changed
}

func (c *controller) buildSetsLocked() DecisionSets {
	var directBuilder netipx.IPSetBuilder
	var proxyBuilder netipx.IPSetBuilder
	for addr, byHost := range c.records {
		direct := false
		proxy := false
		for _, item := range byHost {
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

func (c *controller) publish(sets DecisionSets, generation uint64) {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || generation != c.generation {
		return
	}
	// Keep the state lock until the sink has committed the replacement. This
	// prevents a newer proxy-wins generation from being computed after the
	// generation check but before an older DIRECT set reaches the kernel.
	c.sink(sets)
}

func (c *controller) flush() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.records = make(map[netip.Addr]map[string]record)
	changed := len(c.lastDirect) != 0 || len(c.lastProxy) != 0
	c.lastDirect = nil
	c.lastProxy = nil
	if changed {
		c.generation++
	}
	generation := c.generation
	c.mu.Unlock()
	if changed {
		c.publish(DecisionSets{Direct: &netipx.IPSet{}, Proxy: &netipx.IPSet{}}, generation)
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
