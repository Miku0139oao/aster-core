package statistic

import (
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/atomic"
	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type trafficObserverTest struct {
	mu       sync.Mutex
	inbound  string
	userID   string
	upload   int64
	download int64
}

type trackerManagerTest struct {
	Tracker
	id   string
	info *TrackerInfo
}

func (t *trackerManagerTest) ID() string         { return t.id }
func (t *trackerManagerTest) Info() *TrackerInfo { return t.info }

type reapTracker struct {
	trackerManagerTest
	manager    *Manager
	closeOnce  sync.Once
	closeCount int
	closed     bool
}

func (t *reapTracker) Close() error {
	t.closeOnce.Do(func() {
		t.closed = true
		t.closeCount++
		if t.manager != nil {
			t.manager.Leave(t)
		}
	})
	return nil
}

func newReapTracker(manager *Manager, id string, info *TrackerInfo) *reapTracker {
	return &reapTracker{
		trackerManagerTest: trackerManagerTest{id: id, info: info},
		manager:            manager,
	}
}

func TestManagerReapsIdleZeroByteTCP(t *testing.T) {
	manager := &Manager{}
	stale := newReapTracker(manager, "stale", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-time.Minute),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	})
	live := newReapTracker(manager, "live", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-time.Minute),
		UploadTotal:   atomic.NewInt64(12),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	})
	ike := newReapTracker(manager, "ike", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-time.Minute),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.UDP, DstPort: 500},
	})
	manager.Join(stale)
	manager.Join(live)
	manager.Join(ike)
	if got := manager.reapIdleZeroByteTCP(time.Now()); got != 1 {
		t.Fatalf("reaped %d, want 1", got)
	}
	if !stale.closed || stale.closeCount != 1 {
		t.Fatalf("zero-byte TCP tracker should close exactly once, closed=%v count=%d", stale.closed, stale.closeCount)
	}
	if live.closed || ike.closed {
		t.Fatal("active TCP and UDP/IKE trackers must stay")
	}
	require.Equal(t, 2, manager.ConnectionCount())
}

func TestManagerReapSkipsYoungZeroByteTCP(t *testing.T) {
	manager := &Manager{}
	young := newReapTracker(manager, "young", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-10 * time.Second),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	})
	manager.Join(young)
	require.Zero(t, manager.reapIdleZeroByteTCP(time.Now()))
	require.False(t, young.closed)
	require.Equal(t, 1, manager.ConnectionCount())
}

func TestManagerReapSkipsTCPPorts500And4500(t *testing.T) {
	manager := &Manager{}
	start := time.Now().Add(-time.Minute)
	ike500 := newReapTracker(manager, "ike500", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         start,
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 500},
	})
	ike4500 := newReapTracker(manager, "ike4500", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         start,
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 4500},
	})
	manager.Join(ike500)
	manager.Join(ike4500)
	require.Zero(t, manager.reapIdleZeroByteTCP(time.Now()))
	require.False(t, ike500.closed)
	require.False(t, ike4500.closed)
	require.Equal(t, 2, manager.ConnectionCount())
}

func TestManagerReapCloseIsIdempotent(t *testing.T) {
	manager := &Manager{}
	stale := newReapTracker(manager, "stale", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-time.Minute),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	})
	manager.Join(stale)

	manager.safeReapClose(stale)
	manager.safeReapClose(stale)
	require.True(t, stale.closed)
	require.Equal(t, 1, stale.closeCount)
	require.Zero(t, manager.ConnectionCount())

	// Simulate handleSocket defer Close after the reaper already closed.
	require.NoError(t, stale.Close())
	require.Equal(t, 1, stale.closeCount)
}

func TestManagerReapCloseConcurrentWithHandleSocket(t *testing.T) {
	manager := &Manager{}
	stale := newReapTracker(manager, "stale", &TrackerInfo{
		UUID:          utils.NewUUIDV4(),
		Start:         time.Now().Add(-time.Minute),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	})
	manager.Join(stale)

	done := make(chan struct{})
	go func() {
		manager.safeReapClose(stale)
		close(done)
	}()
	require.NoError(t, stale.Close())
	<-done

	require.True(t, stale.closed)
	require.Equal(t, 1, stale.closeCount)
	require.Zero(t, manager.ConnectionCount())
}

func TestTrackerEligibleForZeroByteReap(t *testing.T) {
	now := time.Now()
	staleStart := now.Add(-time.Minute)

	require.True(t, trackerEligibleForZeroByteReap(&TrackerInfo{
		Start:         staleStart,
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	}, now))
	require.False(t, trackerEligibleForZeroByteReap(&TrackerInfo{
		Start:         now.Add(-10 * time.Second),
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 6651},
	}, now))
	require.False(t, trackerEligibleForZeroByteReap(&TrackerInfo{
		Start:         staleStart,
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.UDP, DstPort: 500},
	}, now))
	require.False(t, trackerEligibleForZeroByteReap(&TrackerInfo{
		Start:         staleStart,
		UploadTotal:   atomic.NewInt64(0),
		DownloadTotal: atomic.NewInt64(0),
		Metadata:      &C.Metadata{NetWork: C.TCP, DstPort: 4500},
	}, now))
}

func TestManagerTracksConnectionCountsByPrincipal(t *testing.T) {
	manager := &Manager{}
	first := &trackerManagerTest{
		id:   "first",
		info: &TrackerInfo{UUID: utils.NewUUIDV4(), Metadata: &C.Metadata{InName: "vless-in", InUser: "user-id"}},
	}
	second := &trackerManagerTest{
		id:   "second",
		info: &TrackerInfo{UUID: utils.NewUUIDV4(), Metadata: &C.Metadata{InName: "vless-in", InUser: "user-id"}},
	}

	manager.Join(first)
	manager.Join(first)
	manager.Join(second)
	require.Equal(t, 2, manager.ConnectionCount())
	require.Equal(t, map[Principal]int{{Inbound: "vless-in", UserID: "user-id"}: 2}, manager.ActiveConnectionsByPrincipal())

	manager.Leave(first)
	manager.Leave(first)
	require.Equal(t, 1, manager.ConnectionCount())
	require.Equal(t, 1, manager.ActiveConnectionsByPrincipal()[Principal{Inbound: "vless-in", UserID: "user-id"}])

	manager.Leave(second)
	require.Zero(t, manager.ConnectionCount())
	require.Empty(t, manager.ActiveConnectionsByPrincipal())
}

func (o *trafficObserverTest) RecordTraffic(inbound, userID string, upload, download int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inbound = inbound
	o.userID = userID
	o.upload += upload
	o.download += download
}

func TestManagerSkipsObserverForUnauthenticatedTraffic(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	manager.PushUploadedFor("TUN", "", 12)
	manager.PushDownloadedFor("TUN", "", 7)

	upload, download := manager.Total()
	require.EqualValues(t, 12, upload)
	require.EqualValues(t, 7, download)
	observer.mu.Lock()
	require.Zero(t, observer.upload)
	require.Zero(t, observer.download)
	observer.mu.Unlock()
}

func TestManagerPushTrafficForPrincipal(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	manager.PushUploadedFor("vless-in", "user-id", 12)
	manager.PushDownloadedFor("vless-in", "user-id", 7)

	upload, download := manager.Total()
	require.EqualValues(t, 12, upload)
	require.EqualValues(t, 7, download)
	observer.mu.Lock()
	require.Equal(t, "vless-in", observer.inbound)
	require.Equal(t, "user-id", observer.userID)
	require.EqualValues(t, 12, observer.upload)
	require.EqualValues(t, 7, observer.download)
	observer.mu.Unlock()

	manager.SetTrafficObserver(nil)
	manager.PushUploadedFor("vless-in", "user-id", 3)
	observer.mu.Lock()
	require.EqualValues(t, 12, observer.upload)
	observer.mu.Unlock()
}

func TestManagerActiveConnectionsForSinglePrincipal(t *testing.T) {
	manager := &Manager{}
	tracked := &trackerManagerTest{
		id:   "tracked",
		info: &TrackerInfo{Metadata: &C.Metadata{InName: "vless-in", InUser: "user-id"}},
	}
	manager.Join(tracked)

	require.Equal(t, 1, manager.ActiveConnections(Principal{Inbound: "vless-in", UserID: "user-id"}))
	require.Zero(t, manager.ActiveConnections(Principal{Inbound: "vless-in", UserID: "other"}))

	manager.Leave(tracked)
	require.Zero(t, manager.ActiveConnections(Principal{Inbound: "vless-in", UserID: "user-id"}))
}

func TestPrincipalReportsOnlyOnceThresholdIsReached(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	p := newPrincipal(&C.Metadata{InName: "vless-in", InUser: "user-id"})
	require.NotNil(t, p)

	p.reportUpload(manager, principalReportThreshold-1)
	observer.mu.Lock()
	require.Zero(t, observer.upload, "traffic below the threshold must not reach the observer")
	observer.mu.Unlock()

	p.reportUpload(manager, 1)
	observer.mu.Lock()
	require.EqualValues(t, principalReportThreshold, observer.upload)
	observer.mu.Unlock()

	// Whatever never reached the threshold must still be reported eventually, so
	// that per-user totals are exact once the connection ends.
	p.reportUpload(manager, 5)
	p.reportDownload(manager, 9)
	p.flush(manager)
	observer.mu.Lock()
	require.EqualValues(t, principalReportThreshold+5, observer.upload)
	require.EqualValues(t, 9, observer.download)
	observer.mu.Unlock()

	// A drained principal has nothing left to report.
	p.flush(manager)
	observer.mu.Lock()
	require.EqualValues(t, principalReportThreshold+5, observer.upload)
	observer.mu.Unlock()
}

func TestPrincipalRequiresAuthenticatedInbound(t *testing.T) {
	require.Nil(t, newPrincipal(nil))
	require.Nil(t, newPrincipal(&C.Metadata{InName: "vless-in"}))
	require.Nil(t, newPrincipal(&C.Metadata{InUser: "user-id"}))
	require.NotNil(t, newPrincipal(&C.Metadata{InName: "vless-in", InUser: "user-id"}))
}

// Accumulated traffic must survive concurrent reporting from the read and write
// directions of the same connection.
func TestPrincipalAccumulatesConcurrently(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	p := newPrincipal(&C.Metadata{InName: "vless-in", InUser: "user-id"})

	const goroutines, perGoroutine, size = 8, 512, 1024
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				p.reportUpload(manager, size)
			}
		}()
	}
	wg.Wait()
	p.flush(manager)

	observer.mu.Lock()
	require.EqualValues(t, goroutines*perGoroutine*size, observer.upload)
	observer.mu.Unlock()
}

func BenchmarkPrincipalReportUpload(b *testing.B) {
	manager := &Manager{}
	manager.SetTrafficObserver(&trafficObserverTest{})
	p := newPrincipal(&C.Metadata{InName: "vless-in", InUser: "user-id"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.reportUpload(manager, 1500)
	}
}

// Each connection accumulates into its own principal, so concurrent connections
// must not contend. Compared against reporting every write straight through to
// the observer, which is what the shared path costs.
func BenchmarkPrincipalReportUploadParallel(b *testing.B) {
	manager := &Manager{}
	manager.SetTrafficObserver(&trafficObserverTest{})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		p := newPrincipal(&C.Metadata{InName: "vless-in", InUser: "user-id"})
		for pb.Next() {
			p.reportUpload(manager, 1500)
		}
	})
}

func BenchmarkManagerPushUploadedForParallel(b *testing.B) {
	manager := &Manager{}
	manager.SetTrafficObserver(&trafficObserverTest{})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			manager.PushUploadedFor("vless-in", "user-id", 1500)
		}
	})
}

func BenchmarkManagerPushUploaded(b *testing.B) {
	manager := &Manager{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		manager.PushUploaded(1500)
	}
}

func BenchmarkManagerPushUploadedForUnauthenticatedTraffic(b *testing.B) {
	manager := &Manager{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		manager.PushUploadedFor("TUN", "", 1500)
	}
}
