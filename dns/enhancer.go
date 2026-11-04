package dns

import (
	"errors"
	"net/netip"

	"github.com/Miku0139oao/aster-core/common/lru"
	"github.com/Miku0139oao/aster-core/component/fakeip"
	C "github.com/Miku0139oao/aster-core/constant"
)

type ResolverEnhancer struct {
	ipv6          bool
	mode          C.DNSMode
	fakeIPPool    *fakeip.Pool
	fakeIPPool6   *fakeip.Pool
	fakeIPSkipper *fakeip.Skipper
	fakeIPTTL     int
	mapping       *lru.LruCache[netip.Addr, string]
	useHosts      bool
}

func (h *ResolverEnhancer) FakeIPEnabled() bool {
	return h.mode == C.DNSFakeIP
}

func (h *ResolverEnhancer) MappingEnabled() bool {
	return h.mode == C.DNSFakeIP || h.mode == C.DNSMapping
}

func (h *ResolverEnhancer) IsExistFakeIP(ip netip.Addr) bool {
	if !h.FakeIPEnabled() {
		return false
	}

	if pool := h.fakeIPPool; pool != nil {
		if pool.Exist(ip) {
			return true
		}
	}

	if pool6 := h.fakeIPPool6; pool6 != nil {
		if pool6.Exist(ip) {
			return true
		}
	}

	return false
}

func (h *ResolverEnhancer) IsFakeIP(ip netip.Addr) bool {
	if !h.FakeIPEnabled() {
		return false
	}

	if pool := h.fakeIPPool; pool != nil {
		if pool.IPNet().Contains(ip) && ip != pool.Gateway() && ip != pool.Broadcast() {
			return true
		}
	}

	if pool6 := h.fakeIPPool6; pool6 != nil {
		if pool6.IPNet().Contains(ip) && ip != pool6.Gateway() && ip != pool6.Broadcast() {
			return true
		}
	}

	return false
}

func (h *ResolverEnhancer) IsFakeBroadcastIP(ip netip.Addr) bool {
	if !h.FakeIPEnabled() {
		return false
	}

	if pool := h.fakeIPPool; pool != nil {
		if pool.Broadcast() == ip {
			return true
		}
	}

	if pool6 := h.fakeIPPool6; pool6 != nil {
		if pool6.Broadcast() == ip {
			return true
		}
	}

	return false
}

func (h *ResolverEnhancer) FindHostByIP(ip netip.Addr) (string, bool) {
	// Fast path: a pool can only hold a mapping for an address inside its own
	// range, and Pool.ipnet is set once at construction and never mutated, so
	// this read needs no lock. That matters because LookBack takes the pool
	// mutex (and the LRU locks inside) and this runs on every UDP packet in
	// fake-ip mode; asking a pool that cannot match serialised unrelated
	// datagrams behind it. The check mirrors IsFakeIP above.
	//
	// The range gate only filters when both pools exist. With a single pool it
	// is skipped entirely: a stale entry left by the old store can sit outside
	// the current range (store-fake-ip persists across range changes) and must
	// still resolve, exactly as before.
	bothPools := h.fakeIPPool != nil && h.fakeIPPool6 != nil
	for _, pool := range [...]*fakeip.Pool{h.fakeIPPool, h.fakeIPPool6} {
		if pool == nil {
			continue
		}
		if bothPools && !pool.IPNet().Contains(ip) {
			continue
		}
		if host, existed := pool.LookBack(ip); existed {
			return host, true
		}
	}

	if mapping := h.mapping; mapping != nil {
		if host, existed := mapping.Get(ip); existed {
			return host, true
		}
	}

	return "", false
}

func (h *ResolverEnhancer) InsertHostByIP(ip netip.Addr, host string) {
	if mapping := h.mapping; mapping != nil {
		h.mapping.Set(ip, host)
	}
}

func (h *ResolverEnhancer) FlushFakeIP() error {
	var errs []error
	if pool := h.fakeIPPool; pool != nil {
		if err := pool.FlushFakeIP(); err != nil {
			errs = append(errs, err)
		}
	}
	if pool6 := h.fakeIPPool6; pool6 != nil {
		if err := pool6.FlushFakeIP(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (h *ResolverEnhancer) PatchFrom(o *ResolverEnhancer) {
	if h.mapping != nil && o.mapping != nil {
		o.mapping.CloneTo(h.mapping)
	}

	if h.fakeIPPool != nil && o.fakeIPPool != nil {
		h.fakeIPPool.CloneFrom(o.fakeIPPool)
	}

	if h.fakeIPPool6 != nil && o.fakeIPPool6 != nil {
		h.fakeIPPool6.CloneFrom(o.fakeIPPool6)
	}
}

func (h *ResolverEnhancer) StoreFakePoolState() {
	if h.fakeIPPool != nil {
		h.fakeIPPool.StoreState()
	}

	if h.fakeIPPool6 != nil {
		h.fakeIPPool6.StoreState()
	}
}

type EnhancerConfig struct {
	IPv6          bool
	EnhancedMode  C.DNSMode
	FakeIPPool    *fakeip.Pool
	FakeIPPool6   *fakeip.Pool
	FakeIPSkipper *fakeip.Skipper
	FakeIPTTL     int
	UseHosts      bool
}

func NewEnhancer(cfg EnhancerConfig) *ResolverEnhancer {
	e := &ResolverEnhancer{
		ipv6:     cfg.IPv6,
		mode:     cfg.EnhancedMode,
		useHosts: cfg.UseHosts,
	}

	if cfg.EnhancedMode != C.DNSNormal {
		e.fakeIPPool = cfg.FakeIPPool
		if cfg.IPv6 {
			e.fakeIPPool6 = cfg.FakeIPPool6
		}
		e.fakeIPSkipper = cfg.FakeIPSkipper
		e.fakeIPTTL = cfg.FakeIPTTL
		if e.fakeIPTTL < 1 {
			e.fakeIPTTL = 1
		}
		e.mapping = lru.New(lru.WithSize[netip.Addr, string](4096))
	}

	return e
}
