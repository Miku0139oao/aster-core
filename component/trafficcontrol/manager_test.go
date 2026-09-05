package trafficcontrol

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func samePolicySlice(left, right []*runtimePolicy) bool {
	return len(left) > 0 && len(right) == len(left) && &left[0] == &right[0]
}

func sameStringSlice(left, right []string) bool {
	return len(left) > 0 && len(right) == len(left) && &left[0] == &right[0]
}

func testManagerConfig(path string) *Config {
	return &Config{
		Enabled: true, StorePath: path, CheckpointInterval: time.Hour, MaxStoreSize: DefaultStoreLimit,
		Reports: ReportsConfig{Enabled: true, HourlyRetention: DefaultHourlyRetention, DailyRetention: DefaultDailyRetention, MonthlyRetention: DefaultMonthlyRetention, OrphanRetention: DefaultOrphanRetention},
		Policies: []Policy{
			{ID: "global", Kind: PolicyGlobal, Enabled: true, Quota: QuotaConfig{TotalBytes: 100, Window: time.Hour, OverageUploadBPS: 64_000, OverageDownloadBPS: 256_000, Portal: true}},
			{ID: "phone", Kind: PolicyDevice, Enabled: true, SourceCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.9/32")}},
			{ID: "rule", Kind: PolicyRule, Enabled: true, Rule: CanonicalRule("DOMAIN-SUFFIX", "example.com", "Proxy")},
			{ID: "target", Kind: PolicyTarget, Enabled: true, Target: TargetSelector{Kind: "group", Name: "Proxy"}},
		},
	}
}

func TestManagerMatchesFourDimensionsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	if err := manager.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	session := manager.Open(Flow{SourceIP: netip.MustParseAddr("192.0.2.9"), RuleType: "DomainSuffix", RulePayload: "example.com", RuleTarget: "Proxy", Chains: []string{"edge", "Proxy"}})
	if session == nil || len(session.binding.Load().policies) != 4 {
		t.Fatalf("expected four matching policies, got %#v", session)
	}
	session.Record(Upload, 60)
	session.Record(Download, 50)
	session.Close()
	status := manager.Status()
	if len(status.Policies) != 4 {
		t.Fatalf("expected four status rows, got %d", len(status.Policies))
	}
	if !status.Policies[0].OverQuota {
		t.Fatal("global quota should be exceeded")
	}
	if err := manager.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	restored := NewManager()
	if err := restored.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	status = restored.Status()
	for _, policy := range status.Policies {
		if policy.Counters.TotalBytes() != 110 {
			t.Fatalf("policy %s restored %d bytes", policy.Policy.ID, policy.Counters.TotalBytes())
		}
	}
}

func TestReportRollupDoesNotDoubleCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	now := time.Date(2026, 8, 6, 10, 30, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	if err := manager.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	session := manager.Open(Flow{})
	session.Record(Upload, 10)
	now = now.Add(2 * time.Hour)
	if err := manager.Flush(); err != nil {
		t.Fatal(err)
	}
	manager.dirty.Store(true)
	if err := manager.Flush(); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	report, err := manager.Reports("global:global", "day", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Counters.UploadBytes != 10 {
		t.Fatalf("unexpected daily rollup: %#v", report)
	}
}

func TestManagerClosePublishesDisabledRuntime(t *testing.T) {
	manager := NewManager()
	require.NoError(t, manager.Configure(testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))))
	require.True(t, manager.Enabled())
	require.NoError(t, manager.Close())
	require.False(t, manager.Enabled())
	require.Empty(t, manager.Status().Policies)
}

func TestResetIsImmediatelyPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	if err := manager.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	session := manager.Open(Flow{})
	session.Record(Upload, 120)
	if err := manager.Reset("global"); err != nil {
		t.Fatal(err)
	}
	if manager.Status().Policies[0].Counters.TotalBytes() != 0 {
		t.Fatal("reset did not clear counters")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaPortalUsesSignedShortLivedURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	config := testManagerConfig(path)
	config.Portal.Listen = "127.0.0.1:0"
	manager := NewManager()
	if err := manager.Configure(config); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	session := manager.Open(Flow{})
	session.Record(Upload, 101)
	portalURL := session.PortalURL()
	if portalURL == "" {
		t.Fatal("expected signed portal URL")
	}
	response, err := http.Get(portalURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("portal returned %d: %s", response.StatusCode, body)
	}
	if len(body) == 0 {
		t.Fatal("portal page is empty")
	}
}

func TestStoreRecoversFromVerifiedBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	if err := manager.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	session := manager.Open(Flow{})
	session.Record(Upload, 42)
	if err := manager.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup was not created: %v", err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered := NewManager()
	if err := recovered.Configure(testManagerConfig(path)); err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.Status().Policies[0].Counters.UploadBytes != 42 {
		t.Fatal("backup did not restore counters")
	}
}

func TestReportsIncludeCurrentHourInDayAndMonth(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 34, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	require.NoError(t, manager.Configure(testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))))
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 17)
	session.Record(Download, 29)
	session.Close()

	dayStart := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	daily, err := manager.Reports("global:global", "day", dayStart, dayStart.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, []UsageBucket{{Start: dayStart.Unix(), Counters: Counters{UploadBytes: 17, DownloadBytes: 29, Connections: 1}}}, daily)

	monthStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	monthly, err := manager.Reports("global:global", "month", monthStart, monthStart.AddDate(0, 1, 0))
	require.NoError(t, err)
	require.Equal(t, []UsageBucket{{Start: monthStart.Unix(), Counters: Counters{UploadBytes: 17, DownloadBytes: 29, Connections: 1}}}, monthly)
}

func TestConfigureAtRevisionAllowsOnlyOneConcurrentUpdate(t *testing.T) {
	manager := NewManager()
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	require.NoError(t, manager.Configure(config))
	defer manager.Close()
	_, revision := manager.Config()

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- manager.ConfigureAtRevision(config, revision)
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.True(t, (first == nil && errors.Is(second, ErrRevisionConflict)) || (second == nil && errors.Is(first, ErrRevisionConflict)))
}

func TestFailedConfigurePreservesActiveLifecycle(t *testing.T) {
	directory := t.TempDir()
	manager := NewManager()
	config := testManagerConfig(filepath.Join(directory, "active.db"))
	config.Portal.Listen = "127.0.0.1:0"
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 101)
	oldPortalURL := session.PortalURL()
	require.NotEmpty(t, oldPortalURL)
	oldRuntime := manager.runtime.Load()
	oldStore := manager.store
	oldPortal := manager.portal.Load()
	_, revision := manager.Config()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	failed := config.Clone()
	failed.StorePath = filepath.Join(directory, "staged.db")
	failed.Portal.Listen = occupied.Addr().String()
	require.Error(t, manager.ConfigureAtRevision(failed, revision))

	current, currentRevision := manager.Config()
	require.Equal(t, revision, currentRevision)
	require.Equal(t, config.StorePath, current.StorePath)
	require.Same(t, oldRuntime, manager.runtime.Load())
	require.Same(t, oldStore, manager.store)
	require.Same(t, oldPortal, manager.portal.Load())
	manager.flusherMu.Lock()
	flusherRunning := manager.flushStop != nil
	manager.flusherMu.Unlock()
	require.True(t, flusherRunning)

	response, err := http.Get(oldPortalURL)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	session.Record(Download, 1)
	require.Equal(t, int64(102), manager.Status().Policies[0].Counters.TotalBytes())
}

func TestConfigureReusesSameStoreAndPortal(t *testing.T) {
	manager := NewManager()
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Portal.Listen = "127.0.0.1:0"
	require.NoError(t, manager.Configure(config))
	defer manager.Close()
	oldStore, oldPortal := manager.store, manager.portal.Load()

	updated := config.Clone()
	updated.Policies[0].UploadBPS = 1_000_000
	require.NoError(t, manager.Configure(updated))
	require.Same(t, oldStore, manager.store)
	require.Same(t, oldPortal, manager.portal.Load())
	require.Equal(t, int64(1_000_000), manager.runtime.Load().policies[0].spec.UploadBPS)
}

func TestActiveSessionRebindsAcrossPolicyGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	config := testManagerConfig(path)
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	firstBinding := session.binding.Load()
	firstState := firstBinding.policies[0].state
	session.Record(Upload, 10)

	sameIdentity := config.Clone()
	sameIdentity.Policies[0].UploadBPS = 1_000_000
	require.NoError(t, manager.Configure(sameIdentity))
	session.Record(Upload, 5)
	secondBinding := session.binding.Load()
	require.NotSame(t, firstBinding.runtime, secondBinding.runtime)
	require.Same(t, firstState, secondBinding.policies[0].state)
	require.Equal(t, int64(1), firstState.Active.Load())
	require.Equal(t, int64(15), secondBinding.policies[0].state.Counters.UploadBytes)

	changedIdentity := sameIdentity.Clone()
	changedIdentity.Policies[0].Quota.Window = 2 * time.Hour
	require.NoError(t, manager.Configure(changedIdentity))
	session.Record(Upload, 7)
	thirdBinding := session.binding.Load()
	thirdState := thirdBinding.policies[0].state
	require.NotSame(t, firstState, thirdState)
	require.Equal(t, int64(0), firstState.Active.Load())
	require.Equal(t, int64(1), thirdState.Active.Load())
	require.Equal(t, int64(7), thirdState.Counters.UploadBytes)

	removed := changedIdentity.Clone()
	removed.Policies = nil
	require.NoError(t, manager.Configure(removed))
	session.Record(Upload, 9)
	require.Empty(t, session.binding.Load().policies)
	require.Equal(t, int64(0), thirdState.Active.Load())
	session.Close()
}

func TestAllowPacketSupportsLowRateDatagramAndRollsBackReservations(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	first := newRuntimePolicy(Policy{ID: "first", Kind: PolicyGlobal, Enabled: true, UploadBPS: 8_000}, &policyState{})
	second := newRuntimePolicy(Policy{ID: "second", Kind: PolicyGlobal, Enabled: true, UploadBPS: 8_000}, &policyState{})
	runtime := &runtimeState{config: &Config{Enabled: true}, policies: []*runtimePolicy{first, second}}
	manager.runtime.Store(runtime)
	session := &Session{manager: manager}
	session.binding.Store(&sessionBinding{runtime: runtime, policies: runtime.policies})

	if !session.AllowPacket(Upload, 1500) {
		t.Fatal("one normal datagram was permanently rejected below 12 kbit/s")
	}
	// Exhaust the second limiter, then verify its rejection does not consume the
	// first policy's tokens.
	if !second.upload.AllowN(now, second.upload.Burst()-1500) {
		t.Fatal("failed to prepare exhausted limiter")
	}
	before := first.upload.TokensAt(now)
	if session.AllowPacket(Upload, 1500) {
		t.Fatal("expected stacked exhausted limiter to reject packet")
	}
	if after := first.upload.TokensAt(now); after != before {
		t.Fatalf("failed stacked admission consumed first limiter tokens: before=%v after=%v", before, after)
	}
}

func TestReportKeysAndSeriesAreBounded(t *testing.T) {
	dimensions := make([]string, maxReportKeysPerSession+50)
	for i := range dimensions {
		dimensions[i] = fmt.Sprintf("device:%d", i)
	}
	if keys := reportKeys(dimensions); len(keys) != maxReportKeysPerSession {
		t.Fatalf("report keys = %d, want %d", len(keys), maxReportKeysPerSession)
	}

	manager := NewManager()
	manager.reports = make(map[string]*reportSeries, maxReportSeries)
	for i := 0; i < maxReportSeries; i++ {
		key := fmt.Sprintf("existing:%d", i)
		manager.reports[key] = &reportSeries{Key: key}
	}
	if series := manager.reportSeries("overflow"); series != nil {
		t.Fatal("report series beyond the global limit was allocated")
	}
}

func TestReportsValidateGranularityBeforeMissingKey(t *testing.T) {
	manager := NewManager()
	if _, err := manager.Reports("missing", "bogus", time.Unix(0, 0), time.Unix(1, 0)); err == nil {
		t.Fatal("missing report key bypassed granularity validation")
	}
}

func TestStatusPolicyCIDRsAreDetached(t *testing.T) {
	manager := NewManager()
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	require.NoError(t, manager.Configure(config))
	defer manager.Close()
	status := manager.Status()
	var device *PolicyStatus
	for i := range status.Policies {
		if status.Policies[i].Policy.ID == "phone" {
			device = &status.Policies[i]
			break
		}
	}
	require.NotNil(t, device)
	device.Policy.SourceCIDRs[0] = netip.MustParsePrefix("203.0.113.0/24")
	status = manager.Status()
	for _, policy := range status.Policies {
		if policy.Policy.ID == "phone" {
			require.Equal(t, "192.0.2.9/32", policy.Policy.SourceCIDRs[0].String())
			return
		}
	}
	t.Fatal("phone policy disappeared")
}

func TestOverQuotaSessionRecoversWhenRollingWindowExpires(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Policies[0].Quota.Window = time.Hour
	require.NoError(t, manager.Configure(config))
	require.NoError(t, manager.stopFlusher())
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 100)
	require.True(t, session.binding.Load().policies[0].state.OverQuota.Load())

	now = now.Add(2 * time.Hour)
	require.True(t, session.AllowPacket(Upload, 1))
	require.False(t, session.binding.Load().policies[0].state.OverQuota.Load())
	session.Close()
}

func TestRecordDoesNotFalseExceedFromStaleRollingWindow(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Policies[0].Quota.TotalBytes = 100
	config.Policies[0].Quota.Window = time.Hour
	require.NoError(t, manager.Configure(config))
	require.NoError(t, manager.stopFlusher())
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 90)
	require.False(t, session.binding.Load().policies[0].state.OverQuota.Load())

	now = now.Add(2 * time.Hour)
	session.Record(Upload, 20)
	state := session.binding.Load().policies[0].state
	require.False(t, state.OverQuota.Load())
	require.Zero(t, state.Counters.ExceededEvents)
	require.Equal(t, int64(20), state.rolling.UploadBytes)
	session.Close()
}

func TestSingletonBindingSharesInternedSlices(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Policies = config.Policies[:1]
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	first := manager.Open(Flow{})
	second := manager.Open(Flow{})
	require.NotNil(t, first)
	require.NotNil(t, second)
	defer first.Close()
	defer second.Close()

	policy := manager.runtime.Load().policies[0]
	firstBinding, secondBinding := first.binding.Load(), second.binding.Load()
	require.True(t, samePolicySlice(policy.self, firstBinding.policies))
	require.True(t, samePolicySlice(firstBinding.policies, secondBinding.policies))
	require.True(t, sameStringSlice(policy.singletonKeys, firstBinding.dimensions))
	require.True(t, sameStringSlice(firstBinding.dimensions, secondBinding.dimensions))
	require.True(t, sameStringSlice(policy.singletonKeys, firstBinding.reportKeys))
	require.True(t, sameStringSlice(firstBinding.reportKeys, secondBinding.reportKeys))
	require.Equal(t, []string{"global:global"}, policy.singletonKeys)
	require.Equal(t, int64(2), policy.state.Active.Load())

	first.Record(Upload, 4)
	second.Record(Download, 6)
	hourly, err := manager.Reports("global:global", "hour", manager.now().Add(-time.Hour), manager.now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []UsageBucket{{
		Start:    manager.now().Unix() / 3600 * 3600,
		Counters: Counters{UploadBytes: 4, DownloadBytes: 6, Connections: 2},
	}}, hourly)
}

func TestMultiMatchBindingsAreIndependentAndDedup(t *testing.T) {
	manager := NewManager()
	require.NoError(t, manager.Configure(testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))))
	defer manager.Close()

	flow := Flow{SourceIP: netip.MustParseAddr("192.0.2.9"), RuleType: "DomainSuffix", RulePayload: "example.com", RuleTarget: "Proxy", Chains: []string{"edge", "Proxy"}}
	first := manager.Open(flow)
	second := manager.Open(flow)
	require.NotNil(t, first)
	require.NotNil(t, second)
	defer first.Close()
	defer second.Close()

	firstBinding, secondBinding := first.binding.Load(), second.binding.Load()
	require.Len(t, firstBinding.policies, 4)
	require.Equal(t, firstBinding.policies, secondBinding.policies)
	require.False(t, samePolicySlice(firstBinding.policies, secondBinding.policies))
	require.False(t, samePolicySlice(firstBinding.policies, firstBinding.policies[0].self))

	wantDimensions := []string{"global:global", "device:phone", "rule:rule", "target:target"}
	require.Equal(t, wantDimensions, firstBinding.dimensions)
	require.Equal(t, wantDimensions, secondBinding.dimensions)
	require.False(t, sameStringSlice(firstBinding.dimensions, secondBinding.dimensions))
	require.Equal(t, reportKeys(wantDimensions), firstBinding.reportKeys)
	require.Equal(t, firstBinding.reportKeys, secondBinding.reportKeys)
	require.False(t, sameStringSlice(firstBinding.reportKeys, secondBinding.reportKeys))

	duplicate := NewManager()
	leftState := &policyState{ID: "global", Buckets: make(map[int64]Counters)}
	rightState := &policyState{ID: "global", Buckets: make(map[int64]Counters)}
	left := newRuntimePolicy(Policy{ID: "global", Kind: PolicyGlobal, Enabled: true}, leftState)
	right := newRuntimePolicy(Policy{ID: "global", Kind: PolicyGlobal, Enabled: true}, rightState)
	duplicate.runtime.Store(&runtimeState{
		config:   &Config{Enabled: true, Reports: ReportsConfig{Enabled: true}},
		policies: []*runtimePolicy{left, right},
	})
	session := duplicate.Open(Flow{})
	require.NotNil(t, session)
	defer session.Close()
	binding := session.binding.Load()
	require.Len(t, binding.policies, 2)
	require.Equal(t, []string{"global:global"}, binding.dimensions)
	require.Equal(t, []string{"global:global"}, binding.reportKeys)
	require.False(t, sameStringSlice(left.singletonKeys, binding.dimensions))
}

func TestOpenMissesAllPolicyKinds(t *testing.T) {
	manager := NewManager()
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Policies[0].Enabled = false
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	require.Nil(t, manager.Open(Flow{}))
	require.Nil(t, manager.Open(Flow{SourceIP: netip.MustParseAddr("198.51.100.9"), RuleType: "DOMAIN", RulePayload: "other.test", RuleTarget: "Direct", Chains: []string{"direct"}}))

	matched := manager.Open(Flow{SourceIP: netip.MustParseAddr("192.0.2.9"), RuleType: "DomainSuffix", RulePayload: "example.com", RuleTarget: "Proxy", Chains: []string{"Proxy"}})
	require.NotNil(t, matched)
	require.Len(t, matched.binding.Load().policies, 3)
	matched.Close()
}

func TestSessionRebindsFromNoMatchBackToPolicies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	manager := NewManager()
	config := testManagerConfig(path)
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 10)
	state := session.binding.Load().policies[0].state
	require.Equal(t, int64(1), state.Active.Load())

	removed := config.Clone()
	removed.Policies = nil
	require.NoError(t, manager.Configure(removed))
	session.Record(Upload, 1)
	empty := session.binding.Load()
	require.NotNil(t, empty)
	require.Same(t, manager.runtime.Load(), empty.runtime)
	require.Empty(t, empty.policies)
	require.Equal(t, int64(0), state.Active.Load())
	require.Equal(t, int64(10), state.Counters.UploadBytes)

	session.Record(Upload, 1)
	require.Same(t, empty, session.binding.Load())

	require.NoError(t, manager.Configure(config))
	session.Record(Upload, 7)
	rebound := session.binding.Load()
	require.NotSame(t, empty, rebound)
	require.Same(t, manager.runtime.Load(), rebound.runtime)
	require.Len(t, rebound.policies, 1)
	require.True(t, samePolicySlice(rebound.policies[0].self, rebound.policies))
	require.Equal(t, int64(1), rebound.policies[0].state.Active.Load())
	require.Equal(t, int64(17), rebound.policies[0].state.Counters.UploadBytes)
	session.Close()
	require.Equal(t, int64(0), rebound.policies[0].state.Active.Load())
}

func TestConcurrentRecordAndConfigure(t *testing.T) {
	manager := NewManager()
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	require.NoError(t, manager.Configure(config))
	defer manager.Close()

	flow := Flow{SourceIP: netip.MustParseAddr("192.0.2.9"), RuleType: "DomainSuffix", RulePayload: "example.com", RuleTarget: "Proxy", Chains: []string{"Proxy"}}
	session := manager.Open(flow)
	require.NotNil(t, session)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 400; i++ {
			// Crossing 100-byte quota requestFlush; identity-changing Configure
			// below reseeds counters so the edge fires repeatedly against flusher restart.
			session.Record(Upload, 60)
			session.Record(Download, 50)
		}
	}()
	for i := 0; i < 40; i++ {
		updated := config.Clone()
		updated.Policies[0].UploadBPS = int64(8_000 + i)
		updated.Policies[0].Quota.Window = time.Hour + time.Duration(i)*time.Minute
		require.NoError(t, manager.Configure(updated))
	}
	<-done
	session.Record(Upload, 101)
	session.Close()
	status := manager.Status()
	require.NotEmpty(t, status.Policies)
	var global *PolicyStatus
	for i := range status.Policies {
		if status.Policies[i].Policy.ID == "global" {
			global = &status.Policies[i]
			break
		}
	}
	require.NotNil(t, global)
	require.True(t, global.OverQuota)
	require.Positive(t, global.Counters.ExceededEvents)
	for _, policy := range status.Policies {
		require.Zero(t, policy.Active)
	}
}

func TestRecordRecoversAfterQuotaWindowWithoutRescanningEveryPacket(t *testing.T) {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	config := testManagerConfig(filepath.Join(t.TempDir(), "traffic.db"))
	config.Policies[0].Quota.Window = time.Hour
	require.NoError(t, manager.Configure(config))
	require.NoError(t, manager.stopFlusher())
	defer manager.Close()

	session := manager.Open(Flow{})
	require.NotNil(t, session)
	session.Record(Upload, 100)
	require.True(t, session.binding.Load().policies[0].state.OverQuota.Load())

	now = now.Add(2 * time.Hour)
	session.Record(Upload, 1)
	state := session.binding.Load().policies[0].state
	require.False(t, state.OverQuota.Load())
	require.Equal(t, int64(1), state.rolling.UploadBytes)
	require.Equal(t, int64(101), state.Counters.UploadBytes)
	session.Close()
}
