package trafficcontrol

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
	if session == nil || len(session.policies) != 4 {
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
	require.True(t, session.policies[0].state.OverQuota.Load())

	now = now.Add(2 * time.Hour)
	require.True(t, session.AllowPacket(Upload, 1))
	require.False(t, session.policies[0].state.OverQuota.Load())
	session.Close()
}
