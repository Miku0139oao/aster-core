package statistic

import (
	"context"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/buf"
	N "github.com/Miku0139oao/aster-core/common/net"
	trafficControl "github.com/Miku0139oao/aster-core/component/trafficcontrol"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type trafficControlTestConn struct {
	N.ExtendedConn
	chains C.Chain
}

func (c *trafficControlTestConn) Chains() C.Chain           { return c.chains }
func (c *trafficControlTestConn) ProviderChains() C.Chain   { return nil }
func (c *trafficControlTestConn) RemoteDestination() string { return "" }
func (c *trafficControlTestConn) AppendToChains(a C.ProxyAdapter) {
	c.chains = append(c.chains, a.Name())
}

func TestTCPTrackerServesQuotaPortalForNewHTTPConnection(t *testing.T) {
	config := &trafficControl.Config{
		Enabled:            true,
		StorePath:          filepath.Join(t.TempDir(), "traffic.db"),
		CheckpointInterval: time.Hour,
		MaxStoreSize:       trafficControl.DefaultStoreLimit,
		Portal:             trafficControl.PortalConfig{Listen: "127.0.0.1:0"},
		Reports: trafficControl.ReportsConfig{
			Enabled:          true,
			HourlyRetention:  trafficControl.DefaultHourlyRetention,
			DailyRetention:   trafficControl.DefaultDailyRetention,
			MonthlyRetention: trafficControl.DefaultMonthlyRetention,
			OrphanRetention:  trafficControl.DefaultOrphanRetention,
		},
		Policies: []trafficControl.Policy{{
			ID:      "global",
			Kind:    trafficControl.PolicyGlobal,
			Enabled: true,
			Quota: trafficControl.QuotaConfig{
				TotalBytes:         1,
				Window:             time.Hour,
				OverageUploadBPS:   64_000,
				OverageDownloadBPS: 256_000,
				Portal:             true,
			},
		}},
	}
	require.NoError(t, trafficControl.Default.Configure(config))
	t.Cleanup(func() { require.NoError(t, trafficControl.Default.Configure(nil)) })

	seed := trafficControl.Default.Open(trafficControl.Flow{})
	require.NotNil(t, seed)
	seed.Record(trafficControl.Upload, 1)
	seed.Close()

	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	conn := &trafficControlTestConn{ExtendedConn: N.NewExtendedConn(client), chains: C.Chain{"DIRECT"}}
	metadata := &C.Metadata{
		NetWork: C.TCP,
		Type:    C.TUN,
		SrcIP:   netip.MustParseAddr("192.0.2.10"),
		Host:    "example.com",
		DstPort: 80,
	}
	tracker := NewTCPTracker(conn, &Manager{}, metadata, nil, 0, 0, false)
	t.Cleanup(func() { _ = tracker.Close() })
	require.NotNil(t, tracker.control)
	require.NotNil(t, tracker.control.session)
	require.NotNil(t, tracker.control.portalResponse)

	buffer := make([]byte, 4096)
	n, err := tracker.Read(buffer)
	require.NoError(t, err)
	response := string(buffer[:n])
	require.True(t, strings.HasPrefix(response, "HTTP/1.1 302 Found\r\n"))
	require.Contains(t, response, "Location: http://")
	require.Contains(t, response, "Cache-Control: no-store")

	written, err := tracker.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	require.NoError(t, err)
	require.Equal(t, len("GET / HTTP/1.1\r\n\r\n"), written)

	writeBuf := buf.NewSize(8)
	t.Cleanup(writeBuf.Release)
	require.NoError(t, tracker.WriteBuffer(writeBuf))

	left := buf.NewSize(64)
	t.Cleanup(left.Release)
	require.ErrorIs(t, tracker.ReadBuffer(left), io.EOF)

	reader, readCounts := tracker.UnwrapReader()
	_, ok := reader.(*controlledReader)
	require.True(t, ok, "portal unwrap reader must be the cached limiter wrapper")
	require.Len(t, readCounts, 1)
	writer, writeCounts := tracker.UnwrapWriter()
	_, ok = writer.(*controlledWriter)
	require.True(t, ok, "portal unwrap writer must be the cached limiter wrapper")
	require.Len(t, writeCounts, 1)
}

func configureEnabledTrafficControl(tb testing.TB) {
	tb.Helper()
	config := &trafficControl.Config{
		Enabled:            true,
		StorePath:          filepath.Join(tb.TempDir(), "traffic.db"),
		CheckpointInterval: time.Hour,
		MaxStoreSize:       trafficControl.DefaultStoreLimit,
		Reports: trafficControl.ReportsConfig{
			Enabled:          true,
			HourlyRetention:  trafficControl.DefaultHourlyRetention,
			DailyRetention:   trafficControl.DefaultDailyRetention,
			MonthlyRetention: trafficControl.DefaultMonthlyRetention,
			OrphanRetention:  trafficControl.DefaultOrphanRetention,
		},
		Policies: []trafficControl.Policy{{
			ID:      "global",
			Kind:    trafficControl.PolicyGlobal,
			Enabled: true,
			Quota: trafficControl.QuotaConfig{
				TotalBytes: 1 << 40,
				Window:     time.Hour,
			},
		}},
	}
	require.NoError(tb, trafficControl.Default.Configure(config))
	tb.Cleanup(func() { require.NoError(tb, trafficControl.Default.Configure(nil)) })
}

func TestTCPTrackerAllocatesControlWhenTrafficEnabled(t *testing.T) {
	configureEnabledTrafficControl(t)
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}, nil, 0, 0, true)
	require.NotNil(t, tracker.control)
	require.NotNil(t, tracker.control.session)
	require.Nil(t, tracker.control.portalResponse)
	require.NotNil(t, tracker.control.ctx)
	require.NotNil(t, tracker.control.cancel)
	require.Equal(t, 1, manager.ConnectionCount())

	ctx := tracker.control.ctx
	require.NoError(t, tracker.Close())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Zero(t, manager.ConnectionCount())
}

func TestTCPTrackerUnwrapUsesControlledWrappersWhenTrafficEnabled(t *testing.T) {
	configureEnabledTrafficControl(t)
	manager := &Manager{}
	tracker := NewTCPTracker(newDiscardTrackerConn(), manager, &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}, nil, 0, 0, true)
	t.Cleanup(func() { _ = tracker.Close() })

	writer, counts := tracker.UnwrapWriter()
	controlled, ok := writer.(*controlledWriter)
	require.True(t, ok)
	require.NotNil(t, controlled.session)
	require.Len(t, counts, 1)

	n, err := writer.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	counts[0](int64(n))
	require.EqualValues(t, 5, tracker.UploadTotal.Load())
	upload, _ := manager.Total()
	require.EqualValues(t, 5, upload)

	reader, readCounts := tracker.UnwrapReader()
	_, ok = reader.(*controlledReader)
	require.True(t, ok)
	require.Len(t, readCounts, 1)

	writer2, counts2 := tracker.UnwrapWriter()
	require.Equal(t, writer, writer2)
	require.Equal(t, len(counts), len(counts2))

	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = tracker.UnwrapWriter()
		_, _ = tracker.UnwrapReader()
	})
	require.Zero(t, allocs)
}

func TestTCPTrackerTrafficControlLifecycleAllocs(t *testing.T) {
	configureEnabledTrafficControl(t)
	manager := &Manager{}
	conn := &staticChainConn{Conn: newDiscardTrackerConn(), chain: C.Chain{"DIRECT"}}
	metadata := &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}
	warm := NewTCPTracker(conn, manager, metadata, nil, 0, 0, true)
	require.NotNil(t, warm.control)
	require.NoError(t, warm.Close())

	allocs := testing.AllocsPerRun(50, func() {
		tracker := NewTCPTracker(conn, manager, metadata, nil, 0, 0, true)
		if tracker.control == nil {
			t.Fatal("TC-on path must allocate tcpControl")
		}
		if err := tracker.Close(); err != nil {
			t.Fatal(err)
		}
	})
	require.Greater(t, allocs, 3.0, "TC-on extra objects include tcpControl, context, and session state; got %v", allocs)
}

func BenchmarkTCPTrackerLifecycleTrafficControl(b *testing.B) {
	configureEnabledTrafficControl(b)
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
