package provider

import (
	"net/netip"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
	"github.com/Miku0139oao/aster-core/rules/common"
)

type RuleSet struct {
	common.Base
	ruleProviderName string
	adapter          string
	isSrc            bool
	noResolveIP      bool
	provider         P.RuleProvider
}

func (rs *RuleSet) RuleType() C.RuleType {
	return C.RuleSet
}

func (rs *RuleSet) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	if provider, ok := rs.getProvider(); ok {
		if rs.isSrc {
			metadata.SwapSrcDst()
			defer metadata.SwapSrcDst()

			helper.ResolveIP = nil // src mode should not resolve ip
		} else if rs.noResolveIP {
			helper.ResolveIP = nil
		}
		return provider.Match(metadata, helper), rs.adapter
	}
	return false, ""
}

// MatchDomain implements C.DomainMatcher
func (rs *RuleSet) MatchDomain(domain string) bool {
	ok, _ := rs.Match(&C.Metadata{Host: domain}, C.RuleMatchHelper{})
	return ok
}

// MatchIp implements C.IpMatcher
func (rs *RuleSet) MatchIp(ip netip.Addr) bool {
	ok, _ := rs.Match(&C.Metadata{DstIP: ip}, C.RuleMatchHelper{})
	return ok
}

func (rs *RuleSet) Adapter() string {
	return rs.adapter
}

func (rs *RuleSet) Payload() string {
	return rs.ruleProviderName
}

func (rs *RuleSet) ProviderNames() []string {
	return []string{rs.ruleProviderName}
}

// KernelDirectMatchSafe reports whether this rule can be evaluated from only
// a DNS name and destination IP without changing its routing meaning.
func (rs *RuleSet) KernelDirectMatchSafe() bool {
	if rs.isSrc {
		return false
	}
	provider, ok := rs.getProvider()
	return ok && provider.Behavior() != P.Classical
}

func (rs *RuleSet) getProvider() (P.RuleProvider, bool) {
	if rs.provider != nil {
		return rs.provider, true
	}
	pp, ok := tunnel.RuleProviders()[rs.ruleProviderName]
	return pp, ok
}

// BindRuleProviders pins this rule to the provider generation parsed with its
// configuration. Old in-flight routing snapshots therefore cannot observe a
// replacement provider from a newer reload.
func (rs *RuleSet) BindRuleProviders(providers map[string]P.RuleProvider) {
	rs.provider = providers[rs.ruleProviderName]
}

func NewRuleSet(ruleProviderName string, adapter string, isSrc bool, noResolveIP bool, providers ...P.RuleProvider) (*RuleSet, error) {
	rs := &RuleSet{
		Base:             common.Base{},
		ruleProviderName: ruleProviderName,
		adapter:          adapter,
		isSrc:            isSrc,
		noResolveIP:      noResolveIP,
	}
	if len(providers) > 0 {
		rs.provider = providers[0]
	}
	return rs, nil
}

var _ C.Rule = (*RuleSet)(nil)
