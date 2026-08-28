package iface

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/component/iface/anet"

	"github.com/metacubex/bart"
)

type Interface struct {
	Index        int
	MTU          int
	Name         string
	HardwareAddr net.HardwareAddr
	Flags        net.Flags
	Addresses    []netip.Prefix
}

var (
	ErrIfaceNotFound = errors.New("interface not found")
	ErrAddrNotFound  = errors.New("addr not found")
)

const ifaceCacheTTL = 20 * time.Second

type ifaceCache struct {
	ifMapByName map[string]*Interface
	ifMapByAddr map[netip.Addr]*Interface
	ifTable     bart.Table[*Interface]
}

type ifaceSnapshot struct {
	cache *ifaceCache
	err   error
	at    time.Time
}

type ifaceFlight struct {
	wg    sync.WaitGroup
	cache *ifaceCache
	err   error
}

var (
	ifaceSnap     atomic.Pointer[ifaceSnapshot]
	ifaceMu       sync.Mutex
	ifaceInflight *ifaceFlight
)

func getCache() (*ifaceCache, error) {
	if snap := ifaceSnap.Load(); snap != nil && time.Since(snap.at) < ifaceCacheTTL {
		return snap.cache, snap.err
	}
	return getCacheSlow()
}

func getCacheSlow() (*ifaceCache, error) {
	ifaceMu.Lock()
	if snap := ifaceSnap.Load(); snap != nil && time.Since(snap.at) < ifaceCacheTTL {
		ifaceMu.Unlock()
		return snap.cache, snap.err
	}
	if f := ifaceInflight; f != nil {
		ifaceMu.Unlock()
		f.wg.Wait()
		return f.cache, f.err
	}
	f := &ifaceFlight{}
	f.wg.Add(1)
	ifaceInflight = f
	ifaceMu.Unlock()

	cache, err := buildIfaceCache()
	f.cache, f.err = cache, err

	ifaceMu.Lock()
	// Same as the old singledo.Reset-during-Do rule: FlushCache must not let this
	// in-flight build republish a snapshot after the caller asked to drop it.
	if ifaceInflight == f {
		ifaceInflight = nil
		ifaceSnap.Store(&ifaceSnapshot{cache: cache, err: err, at: time.Now()})
	}
	ifaceMu.Unlock()
	f.wg.Done()
	return cache, err
}

func buildIfaceCache() (*ifaceCache, error) {
	ifaces, err := anet.Interfaces()
	if err != nil {
		return nil, err
	}

	cache := &ifaceCache{
		ifMapByName: make(map[string]*Interface),
		ifMapByAddr: make(map[netip.Addr]*Interface),
	}

	for _, iface := range ifaces {
		addrs, err := anet.InterfaceAddrsByInterface(&iface)
		if err != nil {
			continue
		}

		ipNets := make([]netip.Prefix, 0, len(addrs))
		for _, addr := range addrs {
			var pf netip.Prefix
			switch ipNet := addr.(type) {
			case *net.IPNet:
				ip, _ := netip.AddrFromSlice(ipNet.IP)
				ones, bits := ipNet.Mask.Size()
				if bits == 32 {
					ip = ip.Unmap()
				}
				pf = netip.PrefixFrom(ip, ones)
			case *net.IPAddr:
				ip, _ := netip.AddrFromSlice(ipNet.IP)
				ip = ip.Unmap()
				pf = netip.PrefixFrom(ip, ip.BitLen())
			}
			if pf.IsValid() {
				ipNets = append(ipNets, pf)
			}
		}

		ifaceObj := &Interface{
			Index:        iface.Index,
			MTU:          iface.MTU,
			Name:         iface.Name,
			HardwareAddr: iface.HardwareAddr,
			Flags:        iface.Flags,
			Addresses:    ipNets,
		}
		cache.ifMapByName[iface.Name] = ifaceObj

		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		for _, prefix := range ipNets {
			cache.ifMapByAddr[prefix.Addr()] = ifaceObj
			cache.ifTable.Insert(prefix, ifaceObj)
		}
	}

	return cache, nil
}

func Interfaces() (map[string]*Interface, error) {
	cache, err := getCache()
	if err != nil {
		return nil, err
	}
	return cache.ifMapByName, nil
}

func ResolveInterface(name string) (*Interface, error) {
	cache, err := getCache()
	if err != nil {
		return nil, err
	}

	iface, ok := cache.ifMapByName[name]
	if !ok {
		return nil, ErrIfaceNotFound
	}

	return iface, nil
}

func ResolveInterfaceByAddr(addr netip.Addr) (*Interface, error) {
	cache, err := getCache()
	if err != nil {
		return nil, err
	}
	// maybe two interfaces have the same prefix but different address
	// so direct check address equal before do a route lookup (longest prefix match)
	if iface, ok := cache.ifMapByAddr[addr]; ok {
		return iface, nil
	}
	iface, ok := cache.ifTable.Lookup(addr)
	if !ok {
		return nil, ErrIfaceNotFound
	}

	return iface, nil
}

func IsLocalIp(addr netip.Addr) (bool, error) {
	cache, err := getCache()
	if err != nil {
		return false, err
	}
	_, ok := cache.ifMapByAddr[addr]
	return ok, nil
}

func FlushCache() {
	ifaceMu.Lock()
	ifaceInflight = nil
	ifaceSnap.Store(nil)
	ifaceMu.Unlock()
}

func (iface *Interface) PickIPv4Addr(destination netip.Addr) (netip.Prefix, error) {
	return iface.pickIPAddr(destination, true)
}

func (iface *Interface) PickIPv6Addr(destination netip.Addr) (netip.Prefix, error) {
	return iface.pickIPAddr(destination, false)
}

func (iface *Interface) pickIPAddr(destination netip.Addr, want4 bool) (netip.Prefix, error) {
	var fallback netip.Prefix

	for _, addr := range iface.Addresses {
		if addr.Addr().Is4() != want4 {
			continue
		}

		if !fallback.IsValid() && !addr.Addr().IsLinkLocalUnicast() {
			fallback = addr

			if !destination.IsValid() {
				break
			}
		}

		if destination.IsValid() && addr.Contains(destination) {
			return addr, nil
		}
	}

	if !fallback.IsValid() {
		return netip.Prefix{}, ErrAddrNotFound
	}

	return fallback, nil
}
