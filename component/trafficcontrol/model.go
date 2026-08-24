package trafficcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultCheckpointInterval = 5 * time.Minute
	DefaultStoreLimit         = int64(64 << 20)
	DefaultOrphanRetention    = 90 * 24 * time.Hour
	DefaultHourlyRetention    = 31 * 24 * time.Hour
	DefaultDailyRetention     = 397 * 24 * time.Hour
	DefaultMonthlyRetention   = 397 * 24 * time.Hour
	DefaultOverageUploadBPS   = int64(64_000)
	DefaultOverageDownloadBPS = int64(256_000)
	minQuotaWindow            = time.Hour
	maxQuotaWindow            = 365 * 24 * time.Hour
)

type RawConfig struct {
	Enabled            bool              `yaml:"enabled" json:"enabled"`
	Store              string            `yaml:"store,omitempty" json:"store,omitempty"`
	CheckpointInterval string            `yaml:"checkpoint-interval,omitempty" json:"checkpoint-interval,omitempty"`
	MaxStoreSize       int64             `yaml:"max-store-size,omitempty" json:"max-store-size,omitempty"`
	Portal             RawPortalConfig   `yaml:"portal,omitempty" json:"portal,omitempty"`
	Reports            RawReportsConfig  `yaml:"reports,omitempty" json:"reports,omitempty"`
	Global             *RawPolicy        `yaml:"global,omitempty" json:"global,omitempty"`
	Devices            []RawDevicePolicy `yaml:"devices,omitempty" json:"devices,omitempty"`
	Rules              []RawRulePolicy   `yaml:"rules,omitempty" json:"rules,omitempty"`
	Targets            []RawTargetPolicy `yaml:"targets,omitempty" json:"targets,omitempty"`
}

type RawPortalConfig struct {
	Listen string `yaml:"listen,omitempty" json:"listen,omitempty"`
	URL    string `yaml:"url,omitempty" json:"url,omitempty"`
}

type RawReportsConfig struct {
	Enabled          *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	HourlyRetention  string `yaml:"hourly-retention,omitempty" json:"hourly-retention,omitempty"`
	DailyRetention   string `yaml:"daily-retention,omitempty" json:"daily-retention,omitempty"`
	MonthlyRetention string `yaml:"monthly-retention,omitempty" json:"monthly-retention,omitempty"`
	OrphanRetention  string `yaml:"orphan-retention,omitempty" json:"orphan-retention,omitempty"`
}

type RawPolicy struct {
	ID          string         `yaml:"id,omitempty" json:"id,omitempty"`
	Name        string         `yaml:"name,omitempty" json:"name,omitempty"`
	Enabled     *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	UploadBPS   int64          `yaml:"upload-bps,omitempty" json:"upload-bps,omitempty"`
	DownloadBPS int64          `yaml:"download-bps,omitempty" json:"download-bps,omitempty"`
	Quota       RawQuotaConfig `yaml:"quota,omitempty" json:"quota,omitempty"`
}

type RawQuotaConfig struct {
	TotalBytes         int64  `yaml:"total-bytes,omitempty" json:"total-bytes,omitempty"`
	UploadBytes        int64  `yaml:"upload-bytes,omitempty" json:"upload-bytes,omitempty"`
	DownloadBytes      int64  `yaml:"download-bytes,omitempty" json:"download-bytes,omitempty"`
	Window             string `yaml:"window,omitempty" json:"window,omitempty"`
	OverageUploadBPS   int64  `yaml:"overage-upload-bps,omitempty" json:"overage-upload-bps,omitempty"`
	OverageDownloadBPS int64  `yaml:"overage-download-bps,omitempty" json:"overage-download-bps,omitempty"`
	Portal             *bool  `yaml:"portal,omitempty" json:"portal,omitempty"`
}

type RawDevicePolicy struct {
	RawPolicy   `yaml:",inline" json:",inline"`
	MAC         string   `yaml:"mac,omitempty" json:"mac,omitempty"`
	SourceCIDRs []string `yaml:"source-cidrs" json:"source-cidrs"`
}

type RawRulePolicy struct {
	RawPolicy `yaml:",inline" json:",inline"`
	Type      string `yaml:"type" json:"type"`
	Payload   string `yaml:"payload,omitempty" json:"payload,omitempty"`
	Target    string `yaml:"target" json:"target"`
}

type RawTargetPolicy struct {
	RawPolicy `yaml:",inline" json:",inline"`
	Kind      string `yaml:"kind" json:"kind"`
	Target    string `yaml:"target" json:"target"`
}

type Config struct {
	Enabled            bool
	StorePath          string
	requestedStore     string
	CheckpointInterval time.Duration
	MaxStoreSize       int64
	Portal             PortalConfig
	Reports            ReportsConfig
	Policies           []Policy
}

type PortalConfig struct {
	Listen string `json:"listen,omitempty"`
	URL    string `json:"url,omitempty"`
}

type ReportsConfig struct {
	Enabled          bool          `json:"enabled"`
	HourlyRetention  time.Duration `json:"hourly_retention"`
	DailyRetention   time.Duration `json:"daily_retention"`
	MonthlyRetention time.Duration `json:"monthly_retention"`
	OrphanRetention  time.Duration `json:"orphan_retention"`
}

type PolicyKind string

const maxTrafficControlPolicies = 256

const (
	PolicyGlobal PolicyKind = "global"
	PolicyDevice PolicyKind = "device"
	PolicyRule   PolicyKind = "rule"
	PolicyTarget PolicyKind = "target"
)

type Policy struct {
	ID          string         `json:"id"`
	Name        string         `json:"name,omitempty"`
	Kind        PolicyKind     `json:"kind"`
	Enabled     bool           `json:"enabled"`
	UploadBPS   int64          `json:"upload_bps,omitempty"`
	DownloadBPS int64          `json:"download_bps,omitempty"`
	Quota       QuotaConfig    `json:"quota"`
	MAC         string         `json:"mac,omitempty"`
	SourceCIDRs []netip.Prefix `json:"source_cidrs,omitempty"`
	Rule        RuleSelector   `json:"rule,omitempty"`
	Target      TargetSelector `json:"target,omitempty"`
}

type QuotaConfig struct {
	TotalBytes         int64         `json:"total_bytes,omitempty"`
	UploadBytes        int64         `json:"upload_bytes,omitempty"`
	DownloadBytes      int64         `json:"download_bytes,omitempty"`
	Window             time.Duration `json:"window,omitempty"`
	OverageUploadBPS   int64         `json:"overage_upload_bps,omitempty"`
	OverageDownloadBPS int64         `json:"overage_download_bps,omitempty"`
	Portal             bool          `json:"portal"`
}

type RuleSelector struct {
	Type        string `json:"type,omitempty"`
	Payload     string `json:"payload,omitempty"`
	Target      string `json:"target,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type TargetSelector struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

type Flow struct {
	SourceIP    netip.Addr
	RuleType    string
	RulePayload string
	RuleTarget  string
	Chains      []string
}

var (
	idPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	macPattern = regexp.MustCompile(`(?i)^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
)

func ParseConfig(raw *RawConfig, resolvePath func(string) (string, error)) (*Config, error) {
	if raw == nil || !raw.Enabled {
		return nil, nil
	}
	if resolvePath == nil {
		return nil, errors.New("traffic-control path resolver is required")
	}
	requestedStore := strings.TrimSpace(raw.Store)
	if requestedStore == "" {
		requestedStore = "traffic-control.db"
	}
	store := requestedStore
	store, err := resolvePath(store)
	if err != nil {
		return nil, fmt.Errorf("traffic-control store: %w", err)
	}
	checkpoint, err := parseOptionalDuration(raw.CheckpointInterval, DefaultCheckpointInterval)
	if err != nil || checkpoint < time.Minute || checkpoint > time.Hour {
		return nil, fmt.Errorf("traffic-control checkpoint-interval must be between 1m and 1h")
	}
	maxStoreSize := raw.MaxStoreSize
	if maxStoreSize == 0 {
		maxStoreSize = DefaultStoreLimit
	}
	if maxStoreSize < 4<<20 || maxStoreSize > 1<<30 {
		return nil, errors.New("traffic-control max-store-size must be between 4 MiB and 1 GiB")
	}
	reports, err := parseReports(raw.Reports)
	if err != nil {
		return nil, err
	}
	policyCount := len(raw.Devices) + len(raw.Rules) + len(raw.Targets)
	if raw.Global != nil {
		policyCount++
	}
	if policyCount > maxTrafficControlPolicies {
		return nil, fmt.Errorf("traffic-control policy count %d exceeds maximum %d", policyCount, maxTrafficControlPolicies)
	}
	result := &Config{
		Enabled: true, StorePath: filepath.Clean(store), requestedStore: requestedStore, CheckpointInterval: checkpoint,
		MaxStoreSize: maxStoreSize, Portal: PortalConfig(raw.Portal), Reports: reports,
	}
	ids := make(map[string]struct{})
	if raw.Global != nil {
		policy, err := parsePolicy(PolicyGlobal, *raw.Global)
		if err != nil {
			return nil, fmt.Errorf("traffic-control global: %w", err)
		}
		if policy.ID == "" {
			policy.ID = "global"
		}
		if err := addPolicy(&result.Policies, ids, policy); err != nil {
			return nil, err
		}
	}
	for i, device := range raw.Devices {
		policy, err := parsePolicy(PolicyDevice, device.RawPolicy)
		if err != nil {
			return nil, fmt.Errorf("traffic-control device %d: %w", i, err)
		}
		if device.MAC != "" {
			device.MAC = strings.ToLower(strings.TrimSpace(device.MAC))
			if !macPattern.MatchString(device.MAC) {
				return nil, fmt.Errorf("traffic-control device %d has invalid MAC", i)
			}
		}
		if policy.ID == "" {
			policy.ID = stableID("device", device.MAC, strings.Join(device.SourceCIDRs, ","))
		}
		policy.MAC = device.MAC
		for _, value := range device.SourceCIDRs {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("traffic-control device %q source-cidrs: %w", policy.ID, err)
			}
			policy.SourceCIDRs = append(policy.SourceCIDRs, prefix.Masked())
		}
		if len(policy.SourceCIDRs) == 0 {
			if policy.MAC != "" {
				return nil, fmt.Errorf("traffic-control device %q MAC matching is not available; source-cidrs is required", policy.ID)
			}
			return nil, fmt.Errorf("traffic-control device %q requires source-cidrs", policy.ID)
		}
		if err := addPolicy(&result.Policies, ids, policy); err != nil {
			return nil, err
		}
	}
	for i, rule := range raw.Rules {
		policy, err := parsePolicy(PolicyRule, rule.RawPolicy)
		if err != nil {
			return nil, fmt.Errorf("traffic-control rule %d: %w", i, err)
		}
		selector := CanonicalRule(rule.Type, rule.Payload, rule.Target)
		if selector.Type == "" || selector.Target == "" {
			return nil, fmt.Errorf("traffic-control rule %d requires type and target", i)
		}
		if policy.ID == "" {
			policy.ID = "rule-" + selector.Fingerprint[:16]
		}
		policy.Rule = selector
		if err := addPolicy(&result.Policies, ids, policy); err != nil {
			return nil, err
		}
	}
	for i, target := range raw.Targets {
		policy, err := parsePolicy(PolicyTarget, target.RawPolicy)
		if err != nil {
			return nil, fmt.Errorf("traffic-control target %d: %w", i, err)
		}
		kind := strings.ToLower(strings.TrimSpace(target.Kind))
		if kind != "proxy" && kind != "group" {
			return nil, fmt.Errorf("traffic-control target %d kind must be proxy or group", i)
		}
		name := strings.TrimSpace(target.Target)
		if name == "" {
			return nil, fmt.Errorf("traffic-control target %d requires target", i)
		}
		if policy.ID == "" {
			policy.ID = stableID("target", kind, name)
		}
		policy.Target = TargetSelector{Kind: kind, Name: name}
		if err := addPolicy(&result.Policies, ids, policy); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parsePolicy(kind PolicyKind, raw RawPolicy) (Policy, error) {
	policy := Policy{ID: strings.TrimSpace(raw.ID), Name: strings.TrimSpace(raw.Name), Kind: kind, Enabled: true, UploadBPS: raw.UploadBPS, DownloadBPS: raw.DownloadBPS}
	if raw.Enabled != nil {
		policy.Enabled = *raw.Enabled
	}
	if policy.UploadBPS < 0 || policy.DownloadBPS < 0 {
		return Policy{}, errors.New("rates cannot be negative")
	}
	quota, err := parseQuota(raw.Quota)
	if err != nil {
		return Policy{}, err
	}
	policy.Quota = quota
	return policy, nil
}

func parseQuota(raw RawQuotaConfig) (QuotaConfig, error) {
	quota := QuotaConfig{TotalBytes: raw.TotalBytes, UploadBytes: raw.UploadBytes, DownloadBytes: raw.DownloadBytes, OverageUploadBPS: raw.OverageUploadBPS, OverageDownloadBPS: raw.OverageDownloadBPS, Portal: true}
	if raw.Portal != nil {
		quota.Portal = *raw.Portal
	}
	if quota.TotalBytes < 0 || quota.UploadBytes < 0 || quota.DownloadBytes < 0 || quota.OverageUploadBPS < 0 || quota.OverageDownloadBPS < 0 {
		return QuotaConfig{}, errors.New("quota values cannot be negative")
	}
	hasQuota := quota.TotalBytes > 0 || quota.UploadBytes > 0 || quota.DownloadBytes > 0
	if hasQuota {
		window, err := parseFlexibleDuration(raw.Window)
		if err != nil || window < minQuotaWindow || window > maxQuotaWindow {
			return QuotaConfig{}, errors.New("quota window must be between 1h and 365d")
		}
		quota.Window = window
		if quota.OverageUploadBPS == 0 {
			quota.OverageUploadBPS = DefaultOverageUploadBPS
		}
		if quota.OverageDownloadBPS == 0 {
			quota.OverageDownloadBPS = DefaultOverageDownloadBPS
		}
	} else if raw.Window != "" {
		return QuotaConfig{}, errors.New("quota window requires a byte limit")
	}
	return quota, nil
}

func parseReports(raw RawReportsConfig) (ReportsConfig, error) {
	reports := ReportsConfig{Enabled: true, HourlyRetention: DefaultHourlyRetention, DailyRetention: DefaultDailyRetention, MonthlyRetention: DefaultMonthlyRetention, OrphanRetention: DefaultOrphanRetention}
	if raw.Enabled != nil {
		reports.Enabled = *raw.Enabled
	}
	values := []struct {
		name, raw string
		target    *time.Duration
	}{
		{"hourly-retention", raw.HourlyRetention, &reports.HourlyRetention},
		{"daily-retention", raw.DailyRetention, &reports.DailyRetention},
		{"monthly-retention", raw.MonthlyRetention, &reports.MonthlyRetention},
		{"orphan-retention", raw.OrphanRetention, &reports.OrphanRetention},
	}
	for _, value := range values {
		if value.raw == "" {
			continue
		}
		duration, err := parseFlexibleDuration(value.raw)
		if err != nil || duration < 24*time.Hour || duration > 10*365*24*time.Hour {
			return ReportsConfig{}, fmt.Errorf("traffic-control reports %s is invalid", value.name)
		}
		*value.target = duration
	}
	return reports, nil
}

func addPolicy(policies *[]Policy, ids map[string]struct{}, policy Policy) error {
	if !idPattern.MatchString(policy.ID) {
		return fmt.Errorf("traffic-control policy id %q is invalid", policy.ID)
	}
	if _, exists := ids[policy.ID]; exists {
		return fmt.Errorf("traffic-control policy id %q is duplicated", policy.ID)
	}
	ids[policy.ID] = struct{}{}
	*policies = append(*policies, policy)
	return nil
}

func CanonicalRule(ruleType, payload, target string) RuleSelector {
	ruleType = canonicalRuleType(ruleType)
	target = strings.TrimSpace(target)
	payload = canonicalPayload(ruleType, payload)
	sum := sha256.Sum256([]byte(ruleType + "\x00" + payload + "\x00" + target))
	return RuleSelector{Type: ruleType, Payload: payload, Target: target, Fingerprint: hex.EncodeToString(sum[:])}
}

func canonicalRuleType(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
}

func canonicalPayload(ruleType, payload string) string {
	payload = strings.TrimSpace(payload)
	switch ruleType {
	case "DOMAIN", "DOMAINSUFFIX", "DOMAINWILDCARD", "GEOSITE", "GEOIP", "SRCGEOIP":
		return strings.ToLower(payload)
	case "IPCIDR", "IPCIDR6", "SRCIPCIDR", "SRCIPCIDR6":
		if prefix, err := netip.ParsePrefix(payload); err == nil {
			return prefix.Masked().String()
		}
	case "DSTPORT", "SRCPORT", "INPORT":
		if number, err := strconv.ParseUint(payload, 10, 16); err == nil {
			return strconv.FormatUint(number, 10)
		}
	}
	return strings.Join(strings.Fields(payload), " ")
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return parts[0] + "-" + hex.EncodeToString(sum[:8])
}

func parseOptionalDuration(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return parseFlexibleDuration(value)
}

func parseFlexibleDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, errors.New("duration is empty")
	}
	var total time.Duration
	for len(value) > 0 {
		i := 0
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == 0 || i == len(value) {
			return 0, errors.New("invalid duration")
		}
		n, err := strconv.ParseInt(value[:i], 10, 64)
		if err != nil || n <= 0 {
			return 0, errors.New("invalid duration")
		}
		j := i
		for j < len(value) && (value[j] < '0' || value[j] > '9') {
			j++
		}
		unit := value[i:j]
		var multiplier time.Duration
		switch unit {
		case "m":
			multiplier = time.Minute
		case "h":
			multiplier = time.Hour
		case "d":
			multiplier = 24 * time.Hour
		case "w":
			multiplier = 7 * 24 * time.Hour
		default:
			return 0, errors.New("invalid duration unit")
		}
		if n > int64((1<<63-1)/multiplier) {
			return 0, errors.New("duration overflows")
		}
		term := time.Duration(n) * multiplier
		if total > time.Duration(1<<63-1)-term {
			return 0, errors.New("duration overflows")
		}
		total += term
		value = value[j:]
	}
	return total, nil
}

func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	clone := *c
	clone.Policies = append([]Policy(nil), c.Policies...)
	for i := range clone.Policies {
		clone.Policies[i].SourceCIDRs = append([]netip.Prefix(nil), c.Policies[i].SourceCIDRs...)
	}
	return &clone
}

// Raw returns the kebab-case document that PUT /policies and YAML parse.
// GET must emit this shape so a client can round-trip GET → PUT.
func (c *Config) Raw() RawConfig {
	if c == nil || !c.Enabled {
		return RawConfig{Enabled: false}
	}
	store := c.requestedStore
	if store == "" {
		store = c.StorePath
	}
	raw := RawConfig{
		Enabled:            true,
		Store:              store,
		CheckpointInterval: formatFlexibleDuration(c.CheckpointInterval),
		MaxStoreSize:       c.MaxStoreSize,
		Portal:             RawPortalConfig{Listen: c.Portal.Listen, URL: c.Portal.URL},
		Reports: RawReportsConfig{
			HourlyRetention:  formatFlexibleDuration(c.Reports.HourlyRetention),
			DailyRetention:   formatFlexibleDuration(c.Reports.DailyRetention),
			MonthlyRetention: formatFlexibleDuration(c.Reports.MonthlyRetention),
			OrphanRetention:  formatFlexibleDuration(c.Reports.OrphanRetention),
		},
	}
	reportsEnabled := c.Reports.Enabled
	raw.Reports.Enabled = &reportsEnabled
	for _, policy := range c.Policies {
		enabled := policy.Enabled
		base := RawPolicy{
			ID:          policy.ID,
			Name:        policy.Name,
			Enabled:     &enabled,
			UploadBPS:   policy.UploadBPS,
			DownloadBPS: policy.DownloadBPS,
			Quota: RawQuotaConfig{
				TotalBytes:         policy.Quota.TotalBytes,
				UploadBytes:        policy.Quota.UploadBytes,
				DownloadBytes:      policy.Quota.DownloadBytes,
				Window:             formatFlexibleDuration(policy.Quota.Window),
				OverageUploadBPS:   policy.Quota.OverageUploadBPS,
				OverageDownloadBPS: policy.Quota.OverageDownloadBPS,
			},
		}
		portal := policy.Quota.Portal
		base.Quota.Portal = &portal
		switch policy.Kind {
		case PolicyGlobal:
			copied := base
			raw.Global = &copied
		case PolicyDevice:
			cidrs := make([]string, 0, len(policy.SourceCIDRs))
			for _, prefix := range policy.SourceCIDRs {
				cidrs = append(cidrs, prefix.String())
			}
			raw.Devices = append(raw.Devices, RawDevicePolicy{RawPolicy: base, MAC: policy.MAC, SourceCIDRs: cidrs})
		case PolicyRule:
			raw.Rules = append(raw.Rules, RawRulePolicy{RawPolicy: base, Type: policy.Rule.Type, Payload: policy.Rule.Payload, Target: policy.Rule.Target})
		case PolicyTarget:
			raw.Targets = append(raw.Targets, RawTargetPolicy{RawPolicy: base, Kind: policy.Target.Kind, Target: policy.Target.Name})
		}
	}
	return raw
}

func formatFlexibleDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%(24*time.Hour) == 0 {
		return strconv.FormatInt(int64(d/(24*time.Hour)), 10) + "d"
	}
	if d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	// parseFlexibleDuration has no second unit; round up so GET → PUT cannot emit "0m".
	minutes := d / time.Minute
	if d%time.Minute != 0 {
		minutes++
	}
	if minutes < 1 {
		minutes = 1
	}
	return strconv.FormatInt(int64(minutes), 10) + "m"
}

func (c *Config) SortedPolicyIDs() []string {
	ids := make([]string, 0, len(c.Policies))
	for _, policy := range c.Policies {
		ids = append(ids, policy.ID)
	}
	sort.Strings(ids)
	return ids
}
