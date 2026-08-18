package tunnel

import (
	"net/netip"

	C "github.com/Miku0139oao/aster-core/constant"
)

type kernelDirectSafeRule interface {
	KernelDirectMatchSafe() bool
}

// ClassifyKernelDirect evaluates only destination-only rules. Returning false
// is always safe: the flow simply follows the normal Aster userspace path.
func (t tunnel) ClassifyKernelDirect(baseMetadata C.Metadata, host string, addr netip.Addr) bool {
	state := routingState.Load()
	switch state.mode {
	case Direct:
		return true
	case Global:
		return false
	}

	metadata := &baseMetadata
	metadata.Host = host
	metadata.DstIP = addr.Unmap()
	metadata.NetWork = C.TCP
	metadata.Type = C.TUN
	if metadata.SpecialProxy != "" {
		return kernelDirectAdapterIsDirect(state, metadata.SpecialProxy)
	}
	unsafeProxyRuleBefore := false
	for _, wrappedRule := range getRules(metadata, state) {
		rule := unwrapKernelDirectRule(wrappedRule)
		if !kernelDirectRuleSafe(rule) {
			if !kernelDirectAdapterIsDirect(state, rule.Adapter()) {
				unsafeProxyRuleBefore = true
			}
			continue
		}

		matched, adapterName := rule.Match(metadata, C.RuleMatchHelper{})
		if !matched {
			continue
		}
		adapter, found := state.proxies[adapterName]
		if !found {
			continue
		}
		switch adapter.Type() {
		case C.Pass, C.PassRule:
			continue
		case C.Direct:
			return !unsafeProxyRuleBefore
		default:
			return false
		}
	}

	// The regular rule engine falls back to DIRECT when no rule matches.
	return !unsafeProxyRuleBefore
}

func unwrapKernelDirectRule(rule C.Rule) C.Rule {
	for {
		wrapper, ok := rule.(C.RuleWrapper)
		if !ok {
			return rule
		}
		rule = wrapper.Unwrap()
	}
}

func kernelDirectRuleSafe(rule C.Rule) bool {
	if safeRule, ok := rule.(kernelDirectSafeRule); ok {
		return safeRule.KernelDirectMatchSafe()
	}
	switch rule.RuleType() {
	case C.Domain, C.DomainSuffix, C.DomainKeyword, C.DomainRegex, C.DomainWildcard,
		C.GEOSITE, C.GEOIP, C.IPASN, C.IPCIDR, C.IPSuffix, C.InName, C.InType, C.MATCH:
		return true
	default:
		return false
	}
}

func kernelDirectAdapterIsDirect(state *routingSnapshot, name string) bool {
	adapter, found := state.proxies[name]
	return found && adapter.Type() == C.Direct
}
