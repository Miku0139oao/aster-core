package cidr

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"

	"go4.org/netipx"
)

type IpCidrSet struct {
	rr     []netipx.IPRange
	merged bool
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
		set.rr = append(set.rr, r)
		set.merged = false
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
		i := sort.Search(len(set.rr), func(i int) bool {
			return ip.Less(set.rr[i].From())
		})
		return i > 0 && set.rr[i-1].Contains(ip)
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
}
