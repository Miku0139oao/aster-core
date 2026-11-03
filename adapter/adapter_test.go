package adapter

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"

	N "github.com/Miku0139oao/aster-core/common/net"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/assert"
)

// testConn gives a bare net.Conn the C.Connection bookkeeping methods needed to
// satisfy C.Conn.
type testConn struct {
	N.ExtendedConn
}

func (c *testConn) Chains() C.Chain                 { return nil }
func (c *testConn) ProviderChains() C.Chain         { return nil }
func (c *testConn) AppendToChains(_ C.ProxyAdapter) {}
func (c *testConn) RemoteDestination() string       { return "" }

// staticDialAdapter hands URLTest a pre-established connection instead of
// dialing, keeping the whole test round trip in memory.
type staticDialAdapter struct {
	C.ProxyAdapter
	conn net.Conn
}

func (a *staticDialAdapter) DialContext(_ context.Context, _ *C.Metadata) (C.Conn, error) {
	return &testConn{ExtendedConn: N.NewExtendedConn(a.conn)}, nil
}

// TestURLTestSubMillisecondDelayIsNotAFailure guards against reporting a
// successful sub-millisecond test as delay 0, which callers treat as a failure
// (maxDelay for groups, HTTP 503 for the API).
func TestURLTestSubMillisecondDelayIsNotAFailure(t *testing.T) {
	const iterations = 20
	for i := 0; i < iterations; i++ {
		clientConn, serverConn := net.Pipe()
		go func() {
			defer serverConn.Close()
			req, err := http.ReadRequest(bufio.NewReader(serverConn))
			if err != nil {
				return
			}
			_ = req.Body.Close()
			_, _ = serverConn.Write([]byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n"))
		}()

		proxy := NewProxy(&staticDialAdapter{conn: clientConn})
		delay, err := proxy.URLTest(context.Background(), "http://example.com", nil)
		if !assert.NoError(t, err, "iteration %d", i) {
			continue
		}
		assert.GreaterOrEqual(t, delay, uint16(1), "iteration %d: a successful test must not report a zero delay", i)
		assert.True(t, proxy.AliveForTestUrl("http://example.com"))
	}
}
