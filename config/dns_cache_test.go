package config

import (
	"strings"
	"testing"
)

func TestParseDNSRejectsNegativeCacheSize(t *testing.T) {
	_, err := parseDNS(&RawConfig{DNS: RawDNS{CacheMaxSize: -1}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cache-max-size") {
		t.Fatalf("error = %v, want negative cache-max-size rejection", err)
	}
}
