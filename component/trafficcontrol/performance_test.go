package trafficcontrol

import (
	"net/netip"
	"testing"
	"time"
)

func benchmarkSession(reportsEnabled bool, quota WindowedQuota) *Session {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	state := &policyState{ID: "global", Buckets: make(map[int64]Counters)}
	policy := newRuntimePolicy(Policy{
		ID: "global", Kind: PolicyGlobal, Enabled: true,
		Quota: QuotaConfig{TotalBytes: quota.TotalBytes, Window: quota.Window},
	}, state)
	runtime := &runtimeState{
		config:   &Config{Enabled: true, Reports: ReportsConfig{Enabled: reportsEnabled}},
		policies: []*runtimePolicy{policy},
	}
	manager.runtime.Store(runtime)
	session := &Session{manager: manager, flow: Flow{}}
	binding := &sessionBinding{runtime: runtime, policies: runtime.policies}
	if reportsEnabled {
		binding.dimensions = []string{"global:global"}
		binding.reportKeys = reportKeys(binding.dimensions)
	}
	session.binding.Store(binding)
	return session
}

type WindowedQuota struct {
	TotalBytes int64
	Window     time.Duration
}

func BenchmarkSessionRecord(b *testing.B) {
	session := benchmarkSession(false, WindowedQuota{TotalBytes: 1 << 62, Window: time.Hour})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Record(Upload, 1500)
	}
}

func BenchmarkSessionRecordReports(b *testing.B) {
	session := benchmarkSession(true, WindowedQuota{TotalBytes: 1 << 62, Window: time.Hour})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Record(Upload, 1500)
	}
}

func BenchmarkSessionRecordNoQuota(b *testing.B) {
	session := benchmarkSession(false, WindowedQuota{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Record(Upload, 1500)
	}
}

func BenchmarkSessionRecordParallel(b *testing.B) {
	session := benchmarkSession(false, WindowedQuota{TotalBytes: 1 << 62, Window: time.Hour})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.Record(Upload, 1500)
		}
	})
}

func openBenchManager(reportsEnabled bool, policies []Policy) *Manager {
	manager := NewManager()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	runtime := &runtimeState{
		config:   &Config{Enabled: true, Reports: ReportsConfig{Enabled: reportsEnabled}},
		policies: make([]*runtimePolicy, 0, len(policies)),
	}
	for _, spec := range policies {
		runtime.policies = append(runtime.policies, newRuntimePolicy(spec, &policyState{ID: spec.ID, Buckets: make(map[int64]Counters)}))
	}
	manager.runtime.Store(runtime)
	return manager
}

func BenchmarkSessionOpen(b *testing.B) {
	global := []Policy{{ID: "global", Kind: PolicyGlobal, Enabled: true}}
	device := []Policy{{ID: "phone", Kind: PolicyDevice, Enabled: true, SourceCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.9/32")}}}
	flow := Flow{}

	b.Run("global", func(b *testing.B) {
		manager := openBenchManager(false, global)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := manager.Open(flow)
			if session != nil {
				session.Close()
			}
		}
	})
	b.Run("miss", func(b *testing.B) {
		manager := openBenchManager(false, device)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := manager.Open(flow)
			if session != nil {
				session.Close()
			}
		}
	})
	b.Run("reports", func(b *testing.B) {
		manager := openBenchManager(true, global)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			session := manager.Open(flow)
			if session != nil {
				session.Close()
			}
		}
	})
}

func BenchmarkSessionRecordManyBuckets(b *testing.B) {
	session := benchmarkSession(false, WindowedQuota{TotalBytes: 1 << 62, Window: time.Hour})
	state := session.binding.Load().policies[0].state
	width := int64(quotaBucketWidth(time.Hour) / time.Second)
	if width < 1 {
		width = 1
	}
	now := session.manager.now().Unix()
	start := now / width * width
	for i := 0; i < 900; i++ {
		state.Buckets[start-int64(i)*width] = Counters{UploadBytes: 1}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Record(Upload, 1500)
	}
}
