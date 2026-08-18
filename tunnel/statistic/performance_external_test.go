package statistic_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/outbound"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/tunnel/statistic"
)

type lifecycleConn struct{}

func (lifecycleConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (lifecycleConn) Write(p []byte) (int, error)      { return len(p), nil }
func (lifecycleConn) Close() error                     { return nil }
func (lifecycleConn) LocalAddr() net.Addr              { return nil }
func (lifecycleConn) RemoteAddr() net.Addr             { return nil }
func (lifecycleConn) SetDeadline(time.Time) error      { return nil }
func (lifecycleConn) SetReadDeadline(time.Time) error  { return nil }
func (lifecycleConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkTCPTrackerLifecycle(b *testing.B) {
	manager := &statistic.Manager{}
	conn := outbound.NewConn(lifecycleConn{}, outbound.NewDirect())
	metadata := &C.Metadata{NetWork: C.TCP, Type: C.TUN, Host: "example.com", DstPort: 443}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker := statistic.NewTCPTracker(conn, manager, metadata, nil, 0, 0, true)
		if err := tracker.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
