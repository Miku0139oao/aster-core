package statistic

import (
	"io"
	"net"
	"testing"

	N "github.com/Miku0139oao/aster-core/common/net"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type trackerTestConn struct {
	N.ExtendedConn
}

func (c *trackerTestConn) Chains() C.Chain               { return nil }
func (c *trackerTestConn) ProviderChains() C.Chain       { return nil }
func (c *trackerTestConn) AppendToChains(C.ProxyAdapter) {}
func (c *trackerTestConn) RemoteDestination() string     { return "" }

// newTrackerTestConn returns a tracked connection plus the peer that drains it.
func newTrackerTestConn(t *testing.T) (C.Conn, net.Conn) {
	t.Helper()
	local, peer := net.Pipe()
	t.Cleanup(func() {
		_ = local.Close()
		_ = peer.Close()
	})
	return &trackerTestConn{ExtendedConn: N.NewExtendedConn(local)}, peer
}

// A tracked connection must report every byte it moved for an authenticated
// user, whether or not the reporting threshold was reached.
func TestTCPTrackerReportsAllBytesForPrincipal(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)

	conn, peer := newTrackerTestConn(t)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(io.Discard, peer)
	}()

	metadata := &C.Metadata{InName: "vless-in", InUser: "user-id"}
	tracker := NewTCPTracker(conn, manager, metadata, nil, 0, 0, true)

	const writes, size = 40, 4096
	payload := make([]byte, size)
	for i := 0; i < writes; i++ {
		n, err := tracker.Write(payload)
		require.NoError(t, err)
		require.Equal(t, size, n)
	}

	// Some of the traffic has crossed the threshold, the remainder is still pending.
	observer.mu.Lock()
	reported := observer.upload
	observer.mu.Unlock()
	require.Positive(t, reported, "traffic beyond the threshold should already be reported")
	require.Less(t, reported, int64(writes*size))

	require.NoError(t, tracker.Close())
	<-drained

	observer.mu.Lock()
	require.Equal(t, "vless-in", observer.inbound)
	require.Equal(t, "user-id", observer.userID)
	require.EqualValues(t, writes*size, observer.upload, "closing must report the pending remainder")
	require.Zero(t, observer.download)
	observer.mu.Unlock()

	require.EqualValues(t, writes*size, tracker.UploadTotal.Load())
	upload, _ := manager.Total()
	require.EqualValues(t, writes*size, upload)
	require.Zero(t, manager.ActiveConnections(Principal{Inbound: "vless-in", UserID: "user-id"}))
}

// Traffic that cannot be attributed to a user must never reach the observer, but
// must still count towards the global totals.
func TestTCPTrackerSkipsObserverWithoutPrincipal(t *testing.T) {
	manager := &Manager{}
	observer := &trafficObserverTest{}
	manager.SetTrafficObserver(observer)

	conn, peer := newTrackerTestConn(t)
	go func() { _, _ = io.Copy(io.Discard, peer) }()

	tracker := NewTCPTracker(conn, manager, &C.Metadata{InName: "TUN"}, nil, 0, 0, true)
	_, err := tracker.Write(make([]byte, principalReportThreshold*2))
	require.NoError(t, err)
	require.NoError(t, tracker.Close())

	observer.mu.Lock()
	require.Zero(t, observer.upload)
	observer.mu.Unlock()
	upload, _ := manager.Total()
	require.EqualValues(t, principalReportThreshold*2, upload)
}
