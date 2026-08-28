package statistic

import (
	"io"
	"net"
	"testing"
	"time"

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

// discardNetConn is a non-blocking Conn so tracker Read/Write benches measure
// statistic overhead rather than pipe or kernel I/O.
type discardNetConn struct{}

func (discardNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (discardNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (discardNetConn) Close() error                     { return nil }
func (discardNetConn) LocalAddr() net.Addr              { return nil }
func (discardNetConn) RemoteAddr() net.Addr             { return nil }
func (discardNetConn) SetDeadline(time.Time) error      { return nil }
func (discardNetConn) SetReadDeadline(time.Time) error  { return nil }
func (discardNetConn) SetWriteDeadline(time.Time) error { return nil }

func newDiscardTrackerConn() C.Conn {
	return &trackerTestConn{ExtendedConn: N.NewExtendedConn(discardNetConn{})}
}

func TestTCPTrackerUnwrapWriterCountsAndReuses(t *testing.T) {
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN}, nil, 0, 0, true)
	t.Cleanup(func() { _ = tracker.Close() })

	writer, counts := tracker.UnwrapWriter()
	require.NotNil(t, writer)
	require.Len(t, counts, 1)

	n, err := writer.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	counts[0](int64(n))

	require.EqualValues(t, 5, tracker.UploadTotal.Load())
	upload, _ := manager.Total()
	require.EqualValues(t, 5, upload)

	writer2, counts2 := tracker.UnwrapWriter()
	require.Equal(t, writer, writer2)
	require.Len(t, counts2, 1)
	require.Equal(t, len(counts), len(counts2))

	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = tracker.UnwrapWriter()
		_, _ = tracker.UnwrapReader()
	})
	require.Zero(t, allocs)
}

func TestTCPTrackerSkipsZeroBytePush(t *testing.T) {
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN}, nil, 0, 0, true)
	t.Cleanup(func() { _ = tracker.Close() })

	n, err := tracker.Read(make([]byte, 16))
	require.Equal(t, 0, n)
	require.ErrorIs(t, err, io.EOF)
	require.Zero(t, tracker.DownloadTotal.Load())
	_, download := manager.Total()
	require.Zero(t, download)
}

type staticChainConn struct {
	C.Conn
	chain C.Chain
}

func (c *staticChainConn) Chains() C.Chain { return c.chain }

func BenchmarkTCPTrackerLifecycle(b *testing.B) {
	manager := &Manager{}
	conn := &staticChainConn{Conn: newDiscardTrackerConn(), chain: C.Chain{"DIRECT"}}
	metadata := &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker := NewTCPTracker(conn, manager, metadata, nil, 0, 0, true)
		if err := tracker.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTCPTrackerWrite(b *testing.B) {
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN}, nil, 0, 0, true)
	b.Cleanup(func() { _ = tracker.Close() })
	payload := make([]byte, 1500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tracker.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTCPTrackerWriteAuthenticated(b *testing.B) {
	manager := &Manager{}
	manager.SetTrafficObserver(&trafficObserverTest{})
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{InName: "vless-in", InUser: "user-id"}, nil, 0, 0, true)
	b.Cleanup(func() { _ = tracker.Close() })
	payload := make([]byte, 1500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tracker.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

var (
	sinkReader io.Reader
	sinkWriter io.Writer
	sinkCounts []N.CountFunc
)

func BenchmarkTCPTrackerUnwrapWriter(b *testing.B) {
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN}, nil, 0, 0, true)
	b.Cleanup(func() { _ = tracker.Close() })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkWriter, sinkCounts = tracker.UnwrapWriter()
	}
}

func BenchmarkTCPTrackerUnwrapReader(b *testing.B) {
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN}, nil, 0, 0, true)
	b.Cleanup(func() { _ = tracker.Close() })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkReader, sinkCounts = tracker.UnwrapReader()
	}
}
