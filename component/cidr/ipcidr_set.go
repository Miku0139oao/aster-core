package cidr

import (
	"errors"
	"fmt"
	"net/netip"

	"go4.org/netipx"
)

type IpCidrSet struct {
	rr     []netipx.IPRange
	merged bool
	// Compact, family-split bounds used only after Merge. Binary search
	// touches from[] then a single to[] compare, without IPRange.Contains.
	v4From []uint32
	v4To   []uint32
	v6From []v6addr
	v6To   []v6addr
}

type v6addr struct {
	hi, lo uint64
}

func NewIpCidrSet() *IpCidrSet {
	return &IpCidrSet{}
}

func (set *IpCidrSet) AddIpCidrForString(ipCidr string) error {
	prefix, err := netip.ParsePrefix(ipCidr)
	if err != nil {
		return err
	}
	return set.AddIpCidr(prefix)
}

func (set *IpCidrSet) AddIpCidr(ipCidr netip.Prefix) (err error) {
	if r := netipx.RangeOfPrefix(ipCidr); r.IsValid() {
		// Drop compact tables before appending so a concurrent or same-
		// package reader cannot observe merged=true with stale v4/v6 bounds.
		set.merged = false
		set.v4From, set.v4To, set.v6From, set.v6To = nil, nil, nil, nil
		set.rr = append(set.rr, r)
	} else {
		err = fmt.Errorf("not valid ipcidr range: %s", ipCidr)
	}
	return
}

func (set *IpCidrSet) IsContainForString(ipString string) bool {
	ip, err := netip.ParseAddr(ipString)
	if err != nil {
		return false
	}
	return set.IsContain(ip)
}

func (set *IpCidrSet) IsContain(ip netip.Addr) bool {
	if set == nil || !ip.IsValid() {
		return false
	}

	ip = ip.WithZone("")
	if set.merged {
		// Merge guarantees sorted, non-overlapping ranges. Find the first range
		// starting after ip, then test its predecessor like netipx.IPSet.Contains.
		if ip.Is4() {
			return containsV4(set.v4From, set.v4To, ipv4Uint32(ip))
		}
		if ip.Is6() {
			return containsV6(set.v6From, set.v6To, ipv6Addr(ip))
		}
		return false
	}
	// Builders and config parsing may query before Merge; preserve that behavior
	// even when ranges were inserted in arbitrary order.
	for _, r := range set.rr {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

// MatchIp implements C.IpMatcher
func (set *IpCidrSet) MatchIp(ip netip.Addr) bool {
	if set.IsEmpty() {
		return false
	}
	return set.IsContain(ip)
}

func (set *IpCidrSet) Merge() error {
	var b netipx.IPSetBuilder
	for _, r := range set.rr {
		if !r.IsValid() {
			return errors.New("invalid IP range")
		}
		b.AddRange(r)
	}
	i, err := b.IPSet()
	if err != nil {
		return err
	}
	set.fromIPSet(i)
	return nil
}

func (set *IpCidrSet) IsEmpty() bool {
	return set == nil || len(set.rr) == 0
}

func (set *IpCidrSet) Foreach(f func(prefix netip.Prefix) bool) {
	for _, r := range set.rr {
		for _, prefix := range r.Prefixes() {
			if !f(prefix) {
				return
			}
		}
	}
}

func (set *IpCidrSet) ToIPSet() *netipx.IPSet {
	if set == nil {
		return new(netipx.IPSet)
	}
	var b netipx.IPSetBuilder
	for _, r := range set.rr {
		if r.IsValid() {
			b.AddRange(r)
		}
	}
	i, err := b.IPSet()
	if err != nil {
		return new(netipx.IPSet)
	}
	return i
}

func (set *IpCidrSet) fromIPSet(i *netipx.IPSet) {
	set.rr = i.Ranges()
	set.merged = true
	set.buildLookup()
}

func (set *IpCidrSet) buildLookup() {
	n4, n6 := 0, 0
	for _, r := range set.rr {
		from := r.From()
		switch {
		case from.Is4():
			n4++
		case from.Is6():
			n6++
		}
	}
	v4From := make([]uint32, 0, n4)
	v4To := make([]uint32, 0, n4)
	v6From := make([]v6addr, 0, n6)
	v6To := make([]v6addr, 0, n6)
	for _, r := range set.rr {
		from, to := r.From(), r.To()
		switch {
		case from.Is4():
			v4From = append(v4From, ipv4Uint32(from))
			v4To = append(v4To, ipv4Uint32(to))
		case from.Is6():
			v6From = append(v6From, ipv6Addr(from))
			v6To = append(v6To, ipv6Addr(to))
		}
	}
	set.v4From, set.v4To = v4From, v4To
	set.v6From, set.v6To = v6From, v6To
}

func ipv4Uint32(ip netip.Addr) uint32 {
	a := ip.As4()
	return uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
}

func ipv6Addr(ip netip.Addr) v6addr {
	a := ip.As16()
	return v6addr{
		hi: uint64(a[0])<<56 | uint64(a[1])<<48 | uint64(a[2])<<40 | uint64(a[3])<<32 |
			uint64(a[4])<<24 | uint64(a[5])<<16 | uint64(a[6])<<8 | uint64(a[7]),
		lo: uint64(a[8])<<56 | uint64(a[9])<<48 | uint64(a[10])<<40 | uint64(a[11])<<32 |
			uint64(a[12])<<24 | uint64(a[13])<<16 | uint64(a[14])<<8 | uint64(a[15]),
	}
}

func (a v6addr) greater(b v6addr) bool {
	return a.hi > b.hi || (a.hi == b.hi && a.lo > b.lo)
}

func (a v6addr) lessOrEqual(b v6addr) bool {
	return a.hi < b.hi || (a.hi == b.hi && a.lo <= b.lo)
}

func containsV4(from, to []uint32, ip uint32) bool {
	i, j := 0, len(from)
	for i < j {
		h := int(uint(i+j) >> 1)
		if from[h] > ip {
			j = h
		} else {
			i = h + 1
		}
	}
	return i > 0 && ip <= to[i-1]
}

func containsV6(from, to []v6addr, ip v6addr) bool {
	i, j := 0, len(from)
	for i < j {
		h := int(uint(i+j) >> 1)
		if from[h].greater(ip) {
			j = h
		} else {
			i = h + 1
		}
	}
	return i > 0 && ip.lessOrEqual(to[i-1])
}
