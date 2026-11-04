package common

import (
	"fmt"
	"sync"

	"github.com/Miku0139oao/aster-core/component/geodata"
	_ "github.com/Miku0139oao/aster-core/component/geodata/memconservative"
	"github.com/Miku0139oao/aster-core/component/geodata/router"
	_ "github.com/Miku0139oao/aster-core/component/geodata/standard"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
)

type GEOSITE struct {
	Base
	country    string
	adapter    string
	recodeSize int

	// matcher caches the resolved domain matcher. Re-resolving it on every
	// evaluation meant doing strings.Split, parseAttrs and a singleflight mutex
	// per rule per connection, which both allocated and serialised under load.
	// The generation guards against a GeoSite database update replacing the
	// underlying data.
	matcherMu       sync.Mutex
	matcher         router.DomainMatcher
	matcherGen      uint64
	matcherResolved bool
}

func (gs *GEOSITE) RuleType() C.RuleType {
	return C.GEOSITE
}

func (gs *GEOSITE) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	return gs.MatchDomain(metadata.RuleHost()), gs.adapter
}

// MatchDomain implements C.DomainMatcher
func (gs *GEOSITE) MatchDomain(domain string) bool {
	if len(domain) == 0 {
		return false
	}
	matcher, err := gs.cachedDomainMatcher()
	if err != nil || matcher == nil {
		return false
	}
	return matcher.ApplyDomain(domain)
}

// cachedDomainMatcher returns the resolved matcher, re-resolving only when the
// GeoSite data has been replaced since it was cached.
func (gs *GEOSITE) cachedDomainMatcher() (router.DomainMatcher, error) {
	gen := geodata.GeoSiteGeneration()
	gs.matcherMu.Lock()
	defer gs.matcherMu.Unlock()
	if gs.matcherResolved && gs.matcherGen == gen {
		return gs.matcher, nil
	}
	matcher, err := gs.GetDomainMatcher()
	if err != nil {
		return nil, err
	}
	gs.matcher = matcher
	gs.matcherGen = gen
	gs.matcherResolved = true
	return matcher, nil
}

func (gs *GEOSITE) Adapter() string {
	return gs.adapter
}

func (gs *GEOSITE) Payload() string {
	return gs.country
}

func (gs *GEOSITE) GetDomainMatcher() (router.DomainMatcher, error) {
	matcher, err := geodata.LoadGeoSiteMatcher(gs.country)
	if err != nil {
		return nil, fmt.Errorf("load GeoSite data error, %w", err)
	}
	return matcher, nil
}

func (gs *GEOSITE) GetRecodeSize() int {
	if matcher, err := gs.GetDomainMatcher(); err == nil {
		return matcher.Count()
	}
	return 0
}

func NewGEOSITE(country string, adapter string) (*GEOSITE, error) {
	if err := geodata.InitGeoSite(); err != nil {
		log.Errorln("can't initial GeoSite: %s", err)
		return nil, err
	}

	geoSite := &GEOSITE{
		Base:    Base{},
		country: country,
		adapter: adapter,
	}

	matcher, err := geoSite.GetDomainMatcher() // test load
	if err != nil {
		return nil, err
	}

	log.Infoln("Finished initial GeoSite rule %s => %s, records: %d", country, adapter, matcher.Count())

	return geoSite, nil
}

var _ C.Rule = (*GEOSITE)(nil)
