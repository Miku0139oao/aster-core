package trafficcontrol

import (
	"path/filepath"
	"testing"
	"time"
)

func boolPointer(value bool) *bool { return &value }

func TestParseConfigBuildsFourPolicyKinds(t *testing.T) {
	raw := &RawConfig{
		Enabled: true,
		Global:  &RawPolicy{UploadBPS: 1_000_000},
		Devices: []RawDevicePolicy{{
			RawPolicy: RawPolicy{ID: "phone", Quota: RawQuotaConfig{TotalBytes: 1000, Window: "1d"}},
			MAC:       "AA:BB:CC:DD:EE:FF", SourceCIDRs: []string{"192.0.2.9/24"},
		}},
		Rules:   []RawRulePolicy{{RawPolicy: RawPolicy{ID: "video"}, Type: "domain-suffix", Payload: " Example.COM ", Target: "Proxy"}},
		Targets: []RawTargetPolicy{{RawPolicy: RawPolicy{ID: "proxy"}, Kind: "group", Target: "Proxy"}},
	}
	config, err := ParseConfig(raw, func(path string) (string, error) { return filepath.Join(t.TempDir(), path), nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Policies) != 4 {
		t.Fatalf("expected four policies, got %d", len(config.Policies))
	}
	if config.Policies[1].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("unexpected MAC %q", config.Policies[1].MAC)
	}
	if got := config.Policies[1].SourceCIDRs[0].String(); got != "192.0.2.0/24" {
		t.Fatalf("CIDR was not masked: %s", got)
	}
	if config.Policies[1].Quota.Window != 24*time.Hour {
		t.Fatalf("unexpected quota window: %s", config.Policies[1].Quota.Window)
	}
	if config.Policies[2].Rule.Payload != "example.com" {
		t.Fatalf("rule was not canonicalized: %#v", config.Policies[2].Rule)
	}
}

func TestCanonicalRuleIsStableAcrossFormatting(t *testing.T) {
	first := CanonicalRule(" ip-cidr ", "192.0.2.99/24", "DIRECT")
	second := CanonicalRule("IP-CIDR", "192.0.2.0/24", "DIRECT")
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprints differ: %s != %s", first.Fingerprint, second.Fingerprint)
	}
}

func TestParseConfigRejectsUnsupportedMACOnlyPolicy(t *testing.T) {
	raw := &RawConfig{Enabled: true, Devices: []RawDevicePolicy{{RawPolicy: RawPolicy{ID: "phone"}, MAC: "aa:bb:cc:dd:ee:ff"}}}
	if _, err := ParseConfig(raw, func(path string) (string, error) { return filepath.Join(t.TempDir(), path), nil }); err == nil {
		t.Fatal("MAC-only policy was accepted even though Flow has no MAC attribution")
	}
}

func TestParseConfigRejectsDuplicateIDs(t *testing.T) {
	raw := &RawConfig{Enabled: true, Devices: []RawDevicePolicy{
		{RawPolicy: RawPolicy{ID: "same"}, SourceCIDRs: []string{"192.0.2.1/32"}},
		{RawPolicy: RawPolicy{ID: "same"}, SourceCIDRs: []string{"192.0.2.2/32"}},
	}}
	_, err := ParseConfig(raw, func(path string) (string, error) { return filepath.Join(t.TempDir(), path), nil })
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestParseConfigKeepsLogicalDefaultStoreForRawAPI(t *testing.T) {
	resolvedRoot := filepath.Join(t.TempDir(), "private-root")
	resolve := func(path string) (string, error) { return filepath.Join(resolvedRoot, path), nil }
	config, err := ParseConfig(&RawConfig{Enabled: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if config.StorePath != filepath.Join(resolvedRoot, "traffic-control.db") {
		t.Fatalf("resolved store = %q", config.StorePath)
	}
	if raw := config.Raw(); raw.Store != "traffic-control.db" {
		t.Fatalf("Raw store exposed resolved path %q", raw.Store)
	}
}

func TestParseFlexibleDurationRejectsCompoundOverflow(t *testing.T) {
	if _, err := parseFlexibleDuration("106751d106751d48h"); err == nil {
		t.Fatal("expected compound duration overflow to fail")
	}
	if _, err := ParseConfig(&RawConfig{Enabled: true, CheckpointInterval: "106751d106751d48h"}, func(path string) (string, error) {
		return filepath.Join(t.TempDir(), path), nil
	}); err == nil {
		t.Fatal("overflowing checkpoint interval was accepted")
	}
}

func TestParseConfigRejectsExcessivePolicyCount(t *testing.T) {
	raw := &RawConfig{Enabled: true, Devices: make([]RawDevicePolicy, maxTrafficControlPolicies+1)}
	_, err := ParseConfig(raw, func(path string) (string, error) { return filepath.Join(t.TempDir(), path), nil })
	if err == nil {
		t.Fatal("excessive policy count was accepted")
	}
}

func TestReportsCanBeDisabled(t *testing.T) {
	raw := &RawConfig{Enabled: true, Reports: RawReportsConfig{Enabled: boolPointer(false)}}
	config, err := ParseConfig(raw, func(path string) (string, error) { return filepath.Join(t.TempDir(), path), nil })
	if err != nil {
		t.Fatal(err)
	}
	if config.Reports.Enabled {
		t.Fatal("reports should be disabled")
	}
}
