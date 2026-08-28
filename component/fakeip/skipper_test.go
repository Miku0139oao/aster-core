package fakeip

import (
	"strings"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/assert"
)

type testHostMatcher struct {
	hosts map[string]struct{}
}

func (m testHostMatcher) MatchDomain(domain string) bool {
	_, ok := m.hosts[strings.ToLower(domain)]
	return ok
}

func newTestHostMatcher(hosts ...string) C.DomainMatcher {
	set := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		set[strings.ToLower(h)] = struct{}{}
	}
	return testHostMatcher{hosts: set}
}

func TestSkipper_BlackList(t *testing.T) {
	skipper := &Skipper{
		Host: []C.DomainMatcher{newTestHostMatcher("example.com")},
	}
	assert.True(t, skipper.ShouldSkipped("example.com"))
	assert.False(t, skipper.ShouldSkipped("foo.com"))
	assert.False(t, skipper.shouldSkipped("baz.com"))
}

func TestSkipper_CaseInsensitive(t *testing.T) {
	skipper := &Skipper{
		Host: []C.DomainMatcher{newTestHostMatcher("example.com")},
	}
	assert.True(t, skipper.ShouldSkipped("EXAMPLE.COM"))
	assert.True(t, skipper.ShouldSkipped("Example.Com"))
	assert.False(t, skipper.ShouldSkipped("FOO.COM"))
}

func TestSkipper_Nil(t *testing.T) {
	var skipper *Skipper
	assert.False(t, skipper.ShouldSkipped("example.com"))
}

func TestSkipper_WhiteList(t *testing.T) {
	skipper := &Skipper{
		Host: []C.DomainMatcher{newTestHostMatcher("example.com")},
		Mode: C.FilterWhiteList,
	}
	assert.False(t, skipper.ShouldSkipped("example.com"))
	assert.True(t, skipper.ShouldSkipped("foo.com"))
	assert.True(t, skipper.ShouldSkipped("baz.com"))
}

type testDomainRule struct {
	host   string
	action string
}

func (r testDomainRule) RuleType() C.RuleType { return C.Domain }
func (r testDomainRule) Match(metadata *C.Metadata, _ C.RuleMatchHelper) (bool, string) {
	if metadata.Host == r.host {
		return true, r.action
	}
	return false, ""
}
func (r testDomainRule) Adapter() string         { return r.action }
func (r testDomainRule) Payload() string         { return r.host }
func (r testDomainRule) ProviderNames() []string { return nil }

func TestSkipper_Rules(t *testing.T) {
	skipper := &Skipper{
		Rules: []C.Rule{
			testDomainRule{host: "real.example", action: UseRealIP},
			testDomainRule{host: "fake.example", action: UseFakeIP},
		},
	}
	assert.True(t, skipper.ShouldSkipped("real.example"))
	assert.True(t, skipper.ShouldSkipped("REAL.EXAMPLE"))
	assert.False(t, skipper.ShouldSkipped("fake.example"))
	assert.False(t, skipper.ShouldSkipped("other.example"))
}

func TestSkipper_RulesReuseMetadata(t *testing.T) {
	skipper := &Skipper{
		Rules: []C.Rule{
			testDomainRule{host: "real.example", action: UseRealIP},
		},
	}
	allocs := testing.AllocsPerRun(1000, func() {
		_ = skipper.ShouldSkipped("real.example")
	})
	if allocs != 0 {
		t.Fatalf("ShouldSkipped allocated %.2f times; want 0 after metadata pool", allocs)
	}
}
