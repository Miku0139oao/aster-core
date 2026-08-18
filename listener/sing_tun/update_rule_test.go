package sing_tun

import (
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"

	"github.com/stretchr/testify/require"
	"go4.org/netipx"
)

type stubAutoRedirect struct {
	updates int
}

func (s *stubAutoRedirect) Start() error           { return nil }
func (s *stubAutoRedirect) Close() error           { return nil }
func (s *stubAutoRedirect) UpdateRouteAddressSet() { s.updates++ }

type cidrStrategy struct {
	set *netipx.IPSet
}

func (c cidrStrategy) ToIpCidr() *netipx.IPSet { return c.set }

type stubRuleProvider struct {
	name     string
	strategy any
}

func (s stubRuleProvider) Name() string               { return s.name }
func (s stubRuleProvider) VehicleType() P.VehicleType { return P.Inline }
func (s stubRuleProvider) Type() P.ProviderType       { return P.Rule }
func (s stubRuleProvider) Initial() error             { return nil }
func (s stubRuleProvider) Update() error              { return nil }
func (s stubRuleProvider) Behavior() P.RuleBehavior   { return P.IPCIDR }
func (s stubRuleProvider) Count() int                 { return 0 }
func (s stubRuleProvider) Match(*C.Metadata, C.RuleMatchHelper) bool {
	return false
}
func (s stubRuleProvider) Strategy() any { return s.strategy }

func TestUpdateRuleSkipsAutoRedirectAfterClose(t *testing.T) {
	var builder netipx.IPSetBuilder
	builder.AddPrefix(netip.MustParsePrefix("1.1.1.1/32"))
	set, err := builder.IPSet()
	require.NoError(t, err)

	redir := &stubAutoRedirect{}
	l := &Listener{
		closed:                 true,
		autoRedirect:           redir,
		routeAddressMap:        map[string]*netipx.IPSet{},
		routeExcludeAddressMap: map[string]*netipx.IPSet{},
	}
	l.updateRule(stubRuleProvider{name: "direct", strategy: cidrStrategy{set: set}}, false, true)
	require.Zero(t, redir.updates)
	require.Contains(t, l.routeAddressMap, "direct")

	l.closed = false
	l.updateRule(stubRuleProvider{name: "direct", strategy: cidrStrategy{set: set}}, false, true)
	require.Equal(t, 1, redir.updates)
}
