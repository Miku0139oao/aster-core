package common

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata"
)

// A GEOSITE rule caches its resolved matcher; re-resolving it per evaluation did
// strings.Split, parseAttrs and a singleflight mutex per rule per connection.
func TestGeoSiteMatcherIsCached(t *testing.T) {
	r, err := NewGEOSITE("cn", "DIRECT")
	if err != nil {
		t.Skipf("geosite data unavailable: %v", err)
	}

	first, err := r.cachedDomainMatcher()
	if err != nil {
		t.Fatalf("cachedDomainMatcher: %v", err)
	}
	again, err := r.cachedDomainMatcher()
	if err != nil {
		t.Fatalf("cachedDomainMatcher: %v", err)
	}
	if first != again {
		t.Fatal("matcher was re-resolved without a data generation change")
	}
}

// A downloaded geodata update replaces the data, so the cached matcher must be
// dropped rather than pinning the old database for the rest of the process.
func TestGeoSiteMatcherIsInvalidatedOnDataUpdate(t *testing.T) {
	r, err := NewGEOSITE("cn", "DIRECT")
	if err != nil {
		t.Skipf("geosite data unavailable: %v", err)
	}
	first, err := r.cachedDomainMatcher()
	if err != nil {
		t.Fatalf("cachedDomainMatcher: %v", err)
	}

	before := geodata.GeoSiteGeneration()
	geodata.ClearGeoSiteCache()
	if after := geodata.GeoSiteGeneration(); after == before {
		t.Fatalf("ClearGeoSiteCache did not bump the generation (still %d)", before)
	}

	reloaded, err := r.cachedDomainMatcher()
	if err != nil {
		t.Fatalf("cachedDomainMatcher after clear: %v", err)
	}
	if reloaded == first {
		t.Fatal("matcher was not re-resolved after the geosite data was cleared")
	}
}

func TestGeoIPGenerationBumpsOnClear(t *testing.T) {
	before := geodata.GeoIPGeneration()
	geodata.ClearGeoIPCache()
	if geodata.GeoIPGeneration() == before {
		t.Fatal("ClearGeoIPCache did not bump the generation")
	}
}
