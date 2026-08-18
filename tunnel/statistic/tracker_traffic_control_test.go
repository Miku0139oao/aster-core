package statistic

import (
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
}
