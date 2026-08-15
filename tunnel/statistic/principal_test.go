package statistic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrincipalAccountantBatchesObserverUpdatesUntilFlush(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	accountant := newPrincipalAccountant(manager, "vless-in", "user-id")

	accountant.addUpload(100)
	accountant.addDownload(40)

	upload, download := manager.Total()
	require.EqualValues(t, 100, upload)
	require.EqualValues(t, 40, download)
	observer.mu.Lock()
	require.Zero(t, observer.upload)
	require.Zero(t, observer.download)
	observer.mu.Unlock()

	accountant.flush()
	observer.mu.Lock()
	require.Equal(t, "vless-in", observer.inbound)
	require.Equal(t, "user-id", observer.userID)
	require.EqualValues(t, 100, observer.upload)
	require.EqualValues(t, 40, observer.download)
	observer.mu.Unlock()
}

func TestPrincipalAccountantFlushesWhenThresholdReached(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	accountant := newPrincipalAccountant(manager, "vless-in", "user-id")

	accountant.addUpload(principalTrafficFlushThreshold - 1)
	observer.mu.Lock()
	require.Zero(t, observer.upload)
	observer.mu.Unlock()

	accountant.addUpload(1)
	observer.mu.Lock()
	require.EqualValues(t, principalTrafficFlushThreshold, observer.upload)
	require.Zero(t, observer.download)
	observer.mu.Unlock()
}

func TestPrincipalAccountantIgnoresUnauthenticatedTraffic(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)
	accountant := newPrincipalAccountant(manager, "TUN", "")

	accountant.addUpload(principalTrafficFlushThreshold)
	accountant.flush()

	upload, _ := manager.Total()
	require.EqualValues(t, principalTrafficFlushThreshold, upload)
	observer.mu.Lock()
	require.Zero(t, observer.upload)
	observer.mu.Unlock()
}
