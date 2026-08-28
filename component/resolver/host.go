package resolver

import (
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	_ "unsafe"

	"github.com/Miku0139oao/aster-core/component/resolver/hosts"
	"github.com/Miku0139oao/aster-core/component/trie"

	"github.com/metacubex/randv2"
)

var (
	DisableSystemHosts, _ = strconv.ParseBool(os.Getenv("DISABLE_SYSTEM_HOSTS"))
	UseSystemHosts        bool
)

type Hosts struct {
	*trie.DomainTrie[HostValue]
}

func NewHosts(hosts *trie.DomainTrie[HostValue]) Hosts {
	return Hosts{
		hosts,
	}
}

const hostAliasHopLimit = 16

// Return the search result and whether to match the parameter `isDomain`
func (h *Hosts) Search(domain string, isDomain bool) (*HostValue, bool) {
	if value := h.DomainTrie.Search(domain); value != nil {
		hostValue := value.Data()
		if !hostValue.IsDomain {
			return &hostValue, !isDomain
		}
		if isDomain {
			return &hostValue, true
		}
		var seen [hostAliasHopLimit]string
		seen[0] = domain
		seenN := 1
		for hops := 0; hops < hostAliasHopLimit; hops++ {
			if !hostValue.IsDomain || hostValue.Domain == "" {
				break
			}
			dup := false
			for i := 0; i < seenN; i++ {
				if seen[i] == hostValue.Domain {
					dup = true
					break
				}
			}
			if dup {
				break
			}
			if seenN < len(seen) {
				seen[seenN] = hostValue.Domain
				seenN++
			}
			node := h.DomainTrie.Search(hostValue.Domain)
			if node == nil {
				break
			}
			hostValue = node.Data()
		}
		return &hostValue, isDomain == hostValue.IsDomain
	}

	if !isDomain && !DisableSystemHosts && UseSystemHosts {
		if ips := hosts.LookupStaticHostIPs(domain); len(ips) > 0 {
			if hostValue, err := NewHostValueByIPs(ips); err == nil {
				return &hostValue, true
			}
		}
	}
	return nil, false
}

type HostValue struct {
	IsDomain bool
	IPs      []netip.Addr
	Domain   string
}

func NewHostValue(value []string) (HostValue, error) {
	isDomain := true
	ips := make([]netip.Addr, 0, len(value))
	domain := ""
	switch len(value) {
	case 0:
		return HostValue{}, errors.New("value is empty")
	case 1:
		host := value[0]
		if ip, err := netip.ParseAddr(host); err == nil {
			ips = append(ips, ip.Unmap())
			isDomain = false
		} else {
			domain = host
		}
	default: // > 1
		isDomain = false
		for _, str := range value {
			if ip, err := netip.ParseAddr(str); err == nil {
				ips = append(ips, ip.Unmap())
			} else {
				return HostValue{}, err
			}
		}
	}
	if isDomain {
		return NewHostValueByDomain(domain)
	}
	return NewHostValueByIPs(ips)
}

func NewHostValueByIPs(ips []netip.Addr) (HostValue, error) {
	if len(ips) == 0 {
		return HostValue{}, errors.New("ip list is empty")
	}
	return HostValue{
		IsDomain: false,
		IPs:      ips,
	}, nil
}

func NewHostValueByDomain(domain string) (HostValue, error) {
	domain = strings.Trim(domain, ".")
	item := strings.Split(domain, ".")
	if len(item) < 2 {
		return HostValue{}, errors.New("invalid domain")
	}
	return HostValue{
		IsDomain: true,
		Domain:   domain,
	}, nil
}

func (hv HostValue) RandIP() (netip.Addr, error) {
	if hv.IsDomain {
		return netip.Addr{}, errors.New("value type is error")
	}
	return hv.IPs[randv2.IntN(len(hv.IPs))], nil
}
