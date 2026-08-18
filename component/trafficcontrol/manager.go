package trafficcontrol

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

var Default = NewManager()

var ErrRevisionConflict = errors.New("traffic-control revision conflict")

type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

type Counters struct {
	UploadBytes    int64 `json:"upload_bytes"`
	DownloadBytes  int64 `json:"download_bytes"`
	Connections    int64 `json:"connections"`
	ExceededEvents int64 `json:"exceeded_events"`
}

func (c *Counters) add(direction Direction, bytes int64) {
	if direction == Upload {
		c.UploadBytes += bytes
	} else {
		c.DownloadBytes += bytes
	}
}

func (c Counters) TotalBytes() int64 { return c.UploadBytes + c.DownloadBytes }

type UsageBucket struct {
	Start    int64    `json:"start"`
	Counters Counters `json:"counters"`
}

type PolicyStatus struct {
	Policy      Policy   `json:"policy"`
	Counters    Counters `json:"counters"`
	Rolling     Counters `json:"rolling"`
	Active      int64    `json:"active_connections"`
	OverQuota   bool     `json:"over_quota"`
	LastUpdated int64    `json:"last_updated"`
	LastReset   int64    `json:"last_reset"`
	Generation  uint64   `json:"generation"`
}

type Status struct {
	Enabled           bool           `json:"enabled"`
	Revision          uint64         `json:"revision"`
	LastCheckpoint    int64          `json:"last_checkpoint"`
	StoreBytes        int64          `json:"store_bytes"`
	UncompressedBytes int64          `json:"uncompressed_bytes"`
	Policies          []PolicyStatus `json:"policies"`
}

type policyState struct {
	mu          sync.Mutex
	ID          string
	Identity    string
	Generation  uint64
	Counters    Counters
	Buckets     map[int64]Counters
	OverQuota   atomic.Bool
	Active      atomic.Int64
	LastUpdated int64
	LastReset   int64
	LastSeen    int64
}

type runtimePolicy struct {
	spec            Policy
	state           *policyState
	upload          *rate.Limiter
	download        *rate.Limiter
	overageUpload   *rate.Limiter
	overageDownload *rate.Limiter
}

type runtimeState struct {
	config   *Config
	policies []*runtimePolicy
}

type reportSeries struct {
	mu      sync.Mutex
	Key     string
	Updated int64
	Hourly  map[int64]Counters
	Daily   map[int64]Counters
	Monthly map[int64]Counters
	Rolled  map[int64]bool
}

type Manager struct {
	configureMu    sync.Mutex
	mu             sync.Mutex
	flusherMu      sync.Mutex
	runtime        atomic.Pointer[runtimeState]
	states         map[string]*policyState
	reportsMu      sync.RWMutex
	reports        map[string]*reportSeries
	store          *Store
	revision       atomic.Uint64
	lastCheckpoint atomic.Int64
	dirty          atomic.Bool
	flushWake      chan struct{}
	flushStop      chan struct{}
	flushDone      chan struct{}
	now            func() time.Time
	portal         atomic.Pointer[portalService]
}

func NewManager() *Manager {
	m := &Manager{states: make(map[string]*policyState), reports: make(map[string]*reportSeries), now: time.Now}
	m.runtime.Store(&runtimeState{})
	return m
}

func (m *Manager) Configure(config *Config) error {
	m.configureMu.Lock()
	defer m.configureMu.Unlock()
	return m.configure(config)
}

// ConfigureAtRevision applies config only when expected is still the active
// revision. It keeps the optimistic-lock check and the lifecycle transition
// in one critical section so concurrent controller updates cannot both win.
func (m *Manager) ConfigureAtRevision(config *Config, expected uint64) error {
	m.configureMu.Lock()
	defer m.configureMu.Unlock()
	if m.revision.Load() != expected {
		return ErrRevisionConflict
	}
	return m.configure(config)
}

func (m *Manager) configure(config *Config) error {
	if err := m.stopFlusher(); err != nil {
		return err
	}
	if err := m.stopPortal(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		if err := m.flushLocked(); err != nil {
			return err
		}
		if err := m.store.Close(); err != nil {
			return err
		}
		m.store = nil
	}
	if config == nil || !config.Enabled {
		m.runtime.Store(&runtimeState{})
		m.states = make(map[string]*policyState)
		m.reportsMu.Lock()
		m.reports = make(map[string]*reportSeries)
		m.reportsMu.Unlock()
		m.revision.Add(1)
		return nil
	}
	config = config.Clone()
	store, err := OpenStore(config.StorePath, config.MaxStoreSize)
	if err != nil {
		return fmt.Errorf("open traffic-control store: %w", err)
	}
	states, reports, checkpoint, err := store.Load()
	if err != nil {
		_ = store.Close()
		return fmt.Errorf("load traffic-control store: %w", err)
	}
	m.store = store
	m.states = states
	m.reportsMu.Lock()
	m.reports = reports
	m.reportsMu.Unlock()
	m.lastCheckpoint.Store(checkpoint)
	now := m.now().Unix()
	runtime := &runtimeState{config: config, policies: make([]*runtimePolicy, 0, len(config.Policies))}
	for _, spec := range config.Policies {
		identity := policyIdentity(spec)
		state := m.states[spec.ID]
		if state == nil {
			state = &policyState{ID: spec.ID, Identity: identity, Generation: 1, Buckets: make(map[int64]Counters), LastReset: now}
			m.states[spec.ID] = state
		} else if state.Identity != identity {
			state = &policyState{ID: spec.ID, Identity: identity, Generation: state.Generation + 1, Buckets: make(map[int64]Counters), LastReset: now}
			m.states[spec.ID] = state
		}
		state.LastSeen = now
		runtime.policies = append(runtime.policies, newRuntimePolicy(spec, state))
	}
	m.cleanupOrphansLocked(config.Reports.OrphanRetention, now)
	m.runtime.Store(runtime)
	m.revision.Add(1)
	m.dirty.Store(true)
	if err := m.startPortal(config.Portal); err != nil {
		_ = store.Close()
		m.store = nil
		m.runtime.Store(&runtimeState{})
		return fmt.Errorf("start traffic-control portal: %w", err)
	}
	m.startFlusher(config.CheckpointInterval)
	return nil
}

func newRuntimePolicy(spec Policy, state *policyState) *runtimePolicy {
	return &runtimePolicy{
		spec: spec, state: state,
		upload: newLimiter(spec.UploadBPS), download: newLimiter(spec.DownloadBPS),
		overageUpload: newLimiter(spec.Quota.OverageUploadBPS), overageDownload: newLimiter(spec.Quota.OverageDownloadBPS),
	}
}

func newLimiter(bitsPerSecond int64) *rate.Limiter {
	if bitsPerSecond <= 0 {
		return nil
	}
	bytesPerSecond := bitsPerSecond / 8
	if bytesPerSecond < 1 {
		bytesPerSecond = 1
	}
	burst := int64(32 << 10)
	if bytesPerSecond < burst {
		burst = bytesPerSecond
	}
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(bytesPerSecond), int(burst))
}

func (m *Manager) Enabled() bool { return m.runtime.Load().config != nil }

func (m *Manager) Config() (*Config, uint64) {
	m.configureMu.Lock()
	defer m.configureMu.Unlock()
	return m.runtime.Load().config.Clone(), m.revision.Load()
}

type Session struct {
	manager    *Manager
	policies   []*runtimePolicy
	dimensions []string
	closed     atomic.Bool
}

func (m *Manager) Open(flow Flow) *Session {
	runtime := m.runtime.Load()
	if runtime.config == nil {
		return nil
	}
	canonicalRule := CanonicalRule(flow.RuleType, flow.RulePayload, flow.RuleTarget)
	policies := make([]*runtimePolicy, 0, 5)
	dimensions := make([]string, 0, 4)
	for _, policy := range runtime.policies {
		if !policy.spec.Enabled || !policyMatches(policy.spec, flow, canonicalRule) {
			continue
		}
		policy.refreshQuota(m.now())
		policies = append(policies, policy)
		policy.state.Active.Add(1)
		dimensions = append(dimensions, string(policy.spec.Kind)+":"+policy.spec.ID)
	}
	if len(policies) == 0 {
		return nil
	}
	session := &Session{manager: m, policies: policies, dimensions: uniqueStrings(dimensions)}
	m.recordReportConnections(session.dimensions, 1)
	return session
}

func policyMatches(policy Policy, flow Flow, rule RuleSelector) bool {
	switch policy.Kind {
	case PolicyGlobal:
		return true
	case PolicyDevice:
		if !flow.SourceIP.IsValid() {
			return false
		}
		for _, prefix := range policy.SourceCIDRs {
			if prefix.Contains(flow.SourceIP.Unmap()) || prefix.Contains(flow.SourceIP) {
				return true
			}
		}
	case PolicyRule:
		return policy.Rule.Fingerprint == rule.Fingerprint
	case PolicyTarget:
		for _, chain := range flow.Chains {
			if chain == policy.Target.Name {
				return true
			}
		}
	}
	return false
}

func (s *Session) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	for _, policy := range s.policies {
		policy.state.Active.Add(-1)
	}
}

func (s *Session) Wait(ctx context.Context, direction Direction, bytes int) error {
	if s == nil || bytes <= 0 {
		return nil
	}
	now := s.manager.now()
	for _, policy := range s.policies {
		if policy.state.OverQuota.Load() {
			policy.refreshQuota(now)
		}
		limiter := policy.limiter(direction)
		if limiter == nil {
			continue
		}
		if err := waitLimiter(ctx, limiter, bytes); err != nil {
			return err
		}
	}
	return nil
}

func (p *runtimePolicy) limiter(direction Direction) *rate.Limiter {
	if p.state.OverQuota.Load() {
		if direction == Upload {
			return p.overageUpload
		}
		return p.overageDownload
	}
	if direction == Upload {
		return p.upload
	}
	return p.download
}

func waitLimiter(ctx context.Context, limiter *rate.Limiter, bytes int) error {
	for bytes > 0 {
		chunk := bytes
		if chunk > limiter.Burst() {
			chunk = limiter.Burst()
		}
		if err := limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		bytes -= chunk
	}
	return nil
}

func (s *Session) AllowPacket(direction Direction, bytes int) bool {
	if s == nil || bytes <= 0 {
		return true
	}
	now := s.manager.now()
	for _, policy := range s.policies {
		if policy.state.OverQuota.Load() {
			policy.refreshQuota(now)
		}
		limiter := policy.limiter(direction)
		if limiter != nil && !limiter.AllowN(now, bytes) {
			return false
		}
	}
	return true
}

func (s *Session) Record(direction Direction, bytes int64) {
	if s == nil || bytes <= 0 {
		return
	}
	now := s.manager.now()
	crossed := false
	for _, policy := range s.policies {
		if policy.record(direction, bytes, now) {
			crossed = true
		}
	}
	s.manager.recordReports(s.dimensions, direction, bytes, now)
	s.manager.dirty.Store(true)
	if crossed {
		s.manager.recordReportExceeded(s.dimensions, now)
		s.manager.requestFlush()
	}
}

func (p *runtimePolicy) refreshQuota(now time.Time) {
	if p.spec.Quota.Window <= 0 {
		p.state.OverQuota.Store(false)
		return
	}
	state := p.state
	state.mu.Lock()
	defer state.mu.Unlock()
	width := quotaBucketWidth(p.spec.Quota.Window)
	cutoff := now.Add(-p.spec.Quota.Window).Unix()
	for key := range state.Buckets {
		if key+int64(width.Seconds()) <= cutoff {
			delete(state.Buckets, key)
		}
	}
	state.OverQuota.Store(quotaExceeded(p.spec.Quota, sumBuckets(state.Buckets)))
}

func (p *runtimePolicy) record(direction Direction, bytes int64, now time.Time) bool {
	state := p.state
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Counters.add(direction, bytes)
	state.LastUpdated = now.Unix()
	state.LastSeen = now.Unix()
	if p.spec.Quota.Window > 0 {
		width := quotaBucketWidth(p.spec.Quota.Window)
		start := now.Unix() / int64(width.Seconds()) * int64(width.Seconds())
		bucket := state.Buckets[start]
		bucket.add(direction, bytes)
		state.Buckets[start] = bucket
		cutoff := now.Add(-p.spec.Quota.Window).Unix()
		for key := range state.Buckets {
			if key+int64(width.Seconds()) <= cutoff {
				delete(state.Buckets, key)
			}
		}
	}
	rolling := sumBuckets(state.Buckets)
	over := quotaExceeded(p.spec.Quota, rolling)
	previous := state.OverQuota.Swap(over)
	if over && !previous {
		state.Counters.ExceededEvents++
	}
	return over && !previous
}

func quotaBucketWidth(window time.Duration) time.Duration {
	width := (window + 1023) / 1024
	if width < time.Minute {
		width = time.Minute
	}
	return width.Round(time.Second)
}

func quotaExceeded(quota QuotaConfig, counters Counters) bool {
	return quota.TotalBytes > 0 && counters.TotalBytes() >= quota.TotalBytes ||
		quota.UploadBytes > 0 && counters.UploadBytes >= quota.UploadBytes ||
		quota.DownloadBytes > 0 && counters.DownloadBytes >= quota.DownloadBytes
}

func sumBuckets(buckets map[int64]Counters) Counters {
	var result Counters
	for _, counter := range buckets {
		result.UploadBytes += counter.UploadBytes
		result.DownloadBytes += counter.DownloadBytes
	}
	return result
}

func (m *Manager) Status() Status {
	runtime := m.runtime.Load()
	status := Status{Enabled: runtime.config != nil, Revision: m.revision.Load(), LastCheckpoint: m.lastCheckpoint.Load()}
	m.mu.Lock()
	if m.store != nil {
		status.StoreBytes = m.store.Size()
		status.UncompressedBytes = m.store.UncompressedBytes()
	}
	m.mu.Unlock()
	for _, runtimePolicy := range runtime.policies {
		runtimePolicy.refreshQuota(m.now())
		state := runtimePolicy.state
		state.mu.Lock()
		item := PolicyStatus{Policy: runtimePolicy.spec, Counters: state.Counters, Rolling: sumBuckets(state.Buckets), Active: state.Active.Load(), OverQuota: state.OverQuota.Load(), LastUpdated: state.LastUpdated, LastReset: state.LastReset, Generation: state.Generation}
		state.mu.Unlock()
		status.Policies = append(status.Policies, item)
	}
	sort.Slice(status.Policies, func(i, j int) bool { return status.Policies[i].Policy.ID < status.Policies[j].Policy.ID })
	return status
}

func (m *Manager) Reset(policyID string) error {
	m.configureMu.Lock()
	defer m.configureMu.Unlock()
	runtime := m.runtime.Load()
	for _, policy := range runtime.policies {
		if policy.spec.ID != policyID {
			continue
		}
		state := policy.state
		state.mu.Lock()
		state.Generation++
		state.Counters = Counters{}
		state.Buckets = make(map[int64]Counters)
		state.LastReset = m.now().Unix()
		state.OverQuota.Store(false)
		state.mu.Unlock()
		m.dirty.Store(true)
		return m.Flush()
	}
	return errors.New("traffic-control policy not found")
}

func policyIdentity(policy Policy) string {
	parts := []string{string(policy.Kind), policy.MAC, policy.Rule.Fingerprint, policy.Target.Kind, policy.Target.Name, policy.Quota.Window.String()}
	for _, prefix := range policy.SourceCIDRs {
		parts = append(parts, prefix.String())
	}
	return stableID(parts...)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := values[:0]
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (m *Manager) requestFlush() {
	select {
	case m.flushWake <- struct{}{}:
	default:
	}
}

func (m *Manager) startFlusher(interval time.Duration) {
	m.flusherMu.Lock()
	defer m.flusherMu.Unlock()
	m.flushWake, m.flushStop, m.flushDone = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(m.flushDone)
		for {
			select {
			case <-ticker.C:
				_ = m.Flush()
			case <-m.flushWake:
				_ = m.Flush()
			case <-m.flushStop:
				return
			}
		}
	}()
}

func (m *Manager) stopFlusher() error {
	m.flusherMu.Lock()
	defer m.flusherMu.Unlock()
	if m.flushStop == nil {
		return nil
	}
	close(m.flushStop)
	<-m.flushDone
	m.flushStop, m.flushDone, m.flushWake = nil, nil, nil
	return nil
}

func (m *Manager) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushLocked()
}

func (m *Manager) flushLocked() error {
	if m.store == nil || !m.dirty.Swap(false) {
		return nil
	}
	now := m.now()
	if err := m.rollupAndPruneLocked(now); err != nil {
		m.dirty.Store(true)
		return err
	}
	m.reportsMu.RLock()
	err := m.store.Save(m.states, m.reports, now.Unix())
	m.reportsMu.RUnlock()
	if errors.Is(err, ErrStoreLimit) {
		err = m.trimReportsToStoreLimitLocked(now.Unix())
	}
	if err != nil {
		m.dirty.Store(true)
		return err
	}
	m.lastCheckpoint.Store(now.Unix())
	return nil
}

func (m *Manager) trimReportsToStoreLimitLocked(checkpoint int64) error {
	for {
		if !m.dropOldestReportSeries() {
			return ErrStoreLimit
		}
		m.reportsMu.RLock()
		err := m.store.Save(m.states, m.reports, checkpoint)
		m.reportsMu.RUnlock()
		if err != nil && !errors.Is(err, ErrStoreLimit) {
			return err
		}
		if compactErr := m.store.Compact(); compactErr != nil {
			return compactErr
		}
		if m.store.Size() <= m.runtime.Load().config.MaxStoreSize {
			return nil
		}
	}
}

func (m *Manager) dropOldestReportSeries() bool {
	m.reportsMu.Lock()
	defer m.reportsMu.Unlock()
	if len(m.reports) == 0 {
		return false
	}
	type candidate struct {
		key     string
		updated int64
	}
	candidates := make([]candidate, 0, len(m.reports))
	for key, series := range m.reports {
		candidates = append(candidates, candidate{key: key, updated: series.Updated})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].updated < candidates[j].updated })
	count := (len(candidates) + 9) / 10
	if count < 1 {
		count = 1
	}
	for _, item := range candidates[:count] {
		delete(m.reports, item.key)
	}
	return true
}

func (m *Manager) cleanupOrphansLocked(retention time.Duration, now int64) {
	cutoff := now - int64(retention.Seconds())
	for id, state := range m.states {
		if state.LastSeen > 0 && state.LastSeen < cutoff {
			delete(m.states, id)
		}
	}
}

func (m *Manager) Close() error {
	m.configureMu.Lock()
	defer m.configureMu.Unlock()
	if err := m.stopFlusher(); err != nil {
		return err
	}
	if err := m.stopPortal(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.flushLocked(); err != nil {
		return err
	}
	if m.store != nil {
		err := m.store.Close()
		m.store = nil
		return err
	}
	return nil
}

func prefixContains(prefix netip.Prefix, address netip.Addr) bool { return prefix.Contains(address) }

func reportPairKey(left, right string) string {
	if left > right {
		left, right = right, left
	}
	return left + "|" + right
}

func reportKeys(dimensions []string) []string {
	keys := append([]string(nil), dimensions...)
	for i := range dimensions {
		for j := i + 1; j < len(dimensions); j++ {
			keys = append(keys, reportPairKey(dimensions[i], dimensions[j]))
		}
	}
	return uniqueStrings(keys)
}

func (m *Manager) reportSeries(key string) *reportSeries {
	m.reportsMu.RLock()
	series := m.reports[key]
	m.reportsMu.RUnlock()
	if series != nil {
		return series
	}
	m.reportsMu.Lock()
	defer m.reportsMu.Unlock()
	if series = m.reports[key]; series == nil {
		series = &reportSeries{Key: key, Hourly: make(map[int64]Counters), Daily: make(map[int64]Counters), Monthly: make(map[int64]Counters), Rolled: make(map[int64]bool)}
		m.reports[key] = series
	}
	return series
}

func (m *Manager) recordReports(dimensions []string, direction Direction, bytes int64, now time.Time) {
	runtime := m.runtime.Load()
	if runtime.config == nil || !runtime.config.Reports.Enabled {
		return
	}
	hour := now.UTC().Truncate(time.Hour).Unix()
	for _, key := range reportKeys(dimensions) {
		series := m.reportSeries(key)
		series.mu.Lock()
		counters := series.Hourly[hour]
		counters.add(direction, bytes)
		series.Hourly[hour] = counters
		series.Updated = now.Unix()
		series.mu.Unlock()
	}
}

func (m *Manager) recordReportConnections(dimensions []string, count int64) {
	runtime := m.runtime.Load()
	if runtime.config == nil || !runtime.config.Reports.Enabled {
		return
	}
	now := m.now()
	hour := now.UTC().Truncate(time.Hour).Unix()
	for _, key := range reportKeys(dimensions) {
		series := m.reportSeries(key)
		series.mu.Lock()
		counters := series.Hourly[hour]
		counters.Connections += count
		series.Hourly[hour] = counters
		series.Updated = now.Unix()
		series.mu.Unlock()
	}
}

func (m *Manager) recordReportExceeded(dimensions []string, now time.Time) {
	runtime := m.runtime.Load()
	if runtime.config == nil || !runtime.config.Reports.Enabled {
		return
	}
	hour := now.UTC().Truncate(time.Hour).Unix()
	for _, key := range reportKeys(dimensions) {
		series := m.reportSeries(key)
		series.mu.Lock()
		counters := series.Hourly[hour]
		counters.ExceededEvents++
		series.Hourly[hour] = counters
		series.Updated = now.Unix()
		series.mu.Unlock()
	}
}

func (m *Manager) rollupAndPruneLocked(now time.Time) error {
	runtime := m.runtime.Load()
	if runtime.config == nil {
		return nil
	}
	reports := runtime.config.Reports
	m.cleanupOrphansLocked(reports.OrphanRetention, now.Unix())
	m.reportsMu.Lock()
	defer m.reportsMu.Unlock()
	for key, series := range m.reports {
		series.mu.Lock()
		for start, counters := range series.Hourly {
			if start < now.UTC().Truncate(time.Hour).Unix() && !series.Rolled[start] {
				t := time.Unix(start, 0).UTC()
				day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix()
				month := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
				series.Daily[day] = addCounters(series.Daily[day], counters)
				series.Monthly[month] = addCounters(series.Monthly[month], counters)
				series.Rolled[start] = true
			}
			if start < now.Add(-reports.HourlyRetention).Unix() {
				delete(series.Hourly, start)
			}
		}
		for start := range series.Rolled {
			if _, exists := series.Hourly[start]; !exists {
				delete(series.Rolled, start)
			}
		}
		for start := range series.Daily {
			if start < now.Add(-reports.DailyRetention).Unix() {
				delete(series.Daily, start)
			}
		}
		for start := range series.Monthly {
			if start < now.Add(-reports.MonthlyRetention).Unix() {
				delete(series.Monthly, start)
			}
		}
		empty := len(series.Hourly) == 0 && len(series.Daily) == 0 && len(series.Monthly) == 0
		series.mu.Unlock()
		if empty {
			delete(m.reports, key)
		}
	}
	return nil
}

func addCounters(left, right Counters) Counters {
	left.UploadBytes += right.UploadBytes
	left.DownloadBytes += right.DownloadBytes
	left.Connections += right.Connections
	left.ExceededEvents += right.ExceededEvents
	return left
}

func (m *Manager) Reports(key, granularity string, from, to time.Time) ([]UsageBucket, error) {
	m.reportsMu.RLock()
	series := m.reports[key]
	m.reportsMu.RUnlock()
	if series == nil {
		return nil, nil
	}
	series.mu.Lock()
	defer series.mu.Unlock()
	var source map[int64]Counters
	switch strings.ToLower(granularity) {
	case "hour":
		source = series.Hourly
	case "day":
		source = cloneCountersMap(series.Daily)
		for start, counters := range series.Hourly {
			if series.Rolled[start] {
				continue
			}
			t := time.Unix(start, 0).UTC()
			bucket := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix()
			source[bucket] = addCounters(source[bucket], counters)
		}
	case "month":
		source = cloneCountersMap(series.Monthly)
		for start, counters := range series.Hourly {
			if series.Rolled[start] {
				continue
			}
			t := time.Unix(start, 0).UTC()
			bucket := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
			source[bucket] = addCounters(source[bucket], counters)
		}
	default:
		return nil, errors.New("invalid report granularity")
	}
	result := make([]UsageBucket, 0)
	for start, counters := range source {
		if start >= from.Unix() && start < to.Unix() {
			result = append(result, UsageBucket{Start: start, Counters: counters})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Start < result[j].Start })
	return result, nil
}
