package statistic

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type trafficObserverTest struct {
	mu       sync.Mutex
	inbound  string
	userID   string
	upload   int64
	download int64
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
