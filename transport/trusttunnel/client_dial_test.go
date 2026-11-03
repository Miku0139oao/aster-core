package trusttunnel

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptrace"
)

// noGotConnRoundTripper blocks until the request context is done and never
// reports an established connection.
type noGotConnRoundTripper struct{}

func (noGotConnRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, request.Context().Err()
}

// blockingRoundTripper reports an established connection but never answers the
// CONNECT, simulating a server that accepted the TCP connection but is slow to
// respond.
type blockingRoundTripper struct{}

func (blockingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	reportGotConn(request)
	<-request.Context().Done()
	return nil, request.Context().Err()
}

func reportGotConn(request *http.Request) {
	trace := httptrace.ContextClientTrace(request.Context())
	if trace != nil && trace.GotConn != nil {
		trace.GotConn(httptrace.GotConnInfo{Conn: &stubConn{}})
	}
}

type stubConn struct{ net.Conn }

func (s *stubConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (s *stubConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (s *stubConn) Close() error                     { return nil }
func (s *stubConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (s *stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (s *stubConn) SetDeadline(time.Time) error      { return nil }
func (s *stubConn) SetReadDeadline(time.Time) error  { return nil }
func (s *stubConn) SetWriteDeadline(time.Time) error { return nil }

func newTestClient(rt http.RoundTripper) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		ctx:          ctx,
		cancel:       cancel,
		server:       "127.0.0.1:443",
		auth:         buildAuth("user", "pass"),
		roundTripper: rt,
	}
	return client
}

// TestDialReturnsAfterConnectResponse replaces the earlier "return as soon as
// GotConn fires" expectation. Returning before the CONNECT response is known
// would hand back a conn that a 407/502 server later fails on first read,
// bypassing the dialer's fallback. Dial must therefore wait for the response,
// and must still be bounded when the server never answers.
func TestDialReturnsAfterConnectResponse(t *testing.T) {
	client := newTestClient(blockingRoundTripper{})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	conn, err := client.Dial(ctx, "example.com:443")
	elapsed := time.Since(start)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("Dial succeeded although the server never answered CONNECT")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Dial blocked for %s on a server that never answers CONNECT", elapsed)
	}
	t.Logf("Dial returned %v after %s", err, elapsed)
}

// TestDialAbortsWhenCallerContextCanceled ensures the caller ctx is propagated
// to the connect wait: cancelling it while the CONNECT is still in flight makes
// Dial return the context error instead of blocking until the transport fails.
func TestDialAbortsWhenCallerContextCanceled(t *testing.T) {
	client := newTestClient(noGotConnRoundTripper{})
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	requestStarted := make(chan struct{})
	client.roundTripper = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	resultCh := make(chan error, 1)
	go func() {
		conn, err := client.Dial(ctx, "example.com:443")
		if conn != nil {
			_ = conn.Close()
		}
		resultCh <- err
	}()

	<-requestStarted
	cancel()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("Dial succeeded after the caller context was canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("Dial did not return after the caller context was canceled")
	}
}

// TestDialHonorsCanceledCallerContext ensures a canceled caller ctx aborts the
// CONNECT attempt and closes the conn instead of returning it.
func TestDialHonorsCanceledCallerContext(t *testing.T) {
	client := newTestClient(blockingRoundTripper{})
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, err := client.Dial(ctx, "example.com:443")
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded with a canceled context")
	}
	if conn != nil {
		t.Fatal("Dial returned a non-nil conn along with the error")
	}
}

// TestDialReturnsRoundTripError ensures RoundTrip failures are reported to the
// caller instead of being swallowed as they were before the fix.
func TestDialReturnsRoundTripError(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, net.ErrClosed
	}))
	defer client.Close()

	conn, err := client.Dial(context.Background(), "example.com:443")
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded although RoundTrip failed")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// multiGotConnRoundTripper reports GotConn more than once (as net/http does on
// connection replacement / redirect) and then fails the request. The dial path
// must not panic on the second signal and must still surface the error.
type multiGotConnRoundTripper struct{ gotConnCalls int }

func (m *multiGotConnRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	reportGotConn(request)
	reportGotConn(request)
	reportGotConn(request)
	m.gotConnCalls++
	return nil, context.Canceled
}

// TestDialSurvivesRepeatedGotConn guards the done-once signalling: closing the
// notification channel directly would panic when GotConn fires twice.
func TestDialSurvivesRepeatedGotConn(t *testing.T) {
	client := newTestClient(&multiGotConnRoundTripper{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := client.Dial(ctx, "example.com:443")
	if conn != nil {
		_ = conn.Close()
	}
	// The first GotConn makes Dial succeed before RoundTrip fails; either
	// outcome is acceptable, but it must never panic.
	_ = err
}
