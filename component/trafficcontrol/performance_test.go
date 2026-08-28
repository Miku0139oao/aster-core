package trafficcontrol

import (
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
