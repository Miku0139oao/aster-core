package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
)

// TestHttpConnectKeepsBytesAfterResponse reproduces a proxy that coalesces its
// CONNECT response with the origin's first bytes in a single Write. Those bytes
// used to sit in a discarded bufio.Reader, so the caller never saw them.
func TestHttpConnectKeepsBytesAfterResponse(t *testing.T) {
	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()

	const payload = "origin-hello"
	proxyErr := make(chan error, 1)
	release := make(chan struct{})

	go func() {
		defer func() { <-release; _ = proxyConn.Close() }()
		request, err := http.ReadRequest(bufio.NewReader(proxyConn))
		if err != nil {
			proxyErr <- fmt.Errorf("read CONNECT request: %w", err)
			return
		}
		_ = request.Body.Close()
		if request.Method != http.MethodConnect {
			proxyErr <- fmt.Errorf("method = %q, want CONNECT", request.Method)
			return
		}
		// Response and origin bytes go out together, as they may over a real
		// proxy connection.
		_, err = proxyConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n" + payload))
		proxyErr <- err
	}()

	h := &Http{
		Base:   NewBase(BaseOption{Name: "test", Addr: "proxy.local:8080", Type: C.Http}),
		option: &HttpOption{},
	}
	metadata := &C.Metadata{Host: "example.com", DstPort: 443}

	conn, err := h.StreamConnContext(context.Background(), clientConn, metadata)
	if err != nil {
		close(release)
		t.Fatalf("StreamConnContext: %v", err)
	}
	if err := <-proxyErr; err != nil {
		close(release)
		t.Fatalf("fake proxy: %v", err)
	}
	// Keep the proxy end open until the read finishes: net.Pipe connections
	// reject SetReadDeadline once the other side is closed.
	defer close(release)

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read origin bytes that arrived with the CONNECT response: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("origin bytes = %q, want %q", got, payload)
	}

	// The buffered bytes must have been served by the returned conn.
	if buffered, ok := conn.(interface{ Buffered() int }); !ok {
		t.Fatalf("returned conn is %T, want a buffered conn", conn)
	} else if buffered.Buffered() != 0 {
		t.Fatalf("buffered bytes left over = %d, want 0", buffered.Buffered())
	}
}

func TestHttpConnectErrorResponse(t *testing.T) {
	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()

	go func() {
		defer proxyConn.Close()
		if _, err := http.ReadRequest(bufio.NewReader(proxyConn)); err != nil {
			return
		}
		_, _ = proxyConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
	}()

	h := &Http{
		Base:   NewBase(BaseOption{Name: "test", Addr: "proxy.local:8080", Type: C.Http}),
		option: &HttpOption{},
	}
	_, err := h.StreamConnContext(context.Background(), clientConn, &C.Metadata{Host: "example.com", DstPort: 443})
	if err == nil {
		t.Fatal("expected an error for a 502 CONNECT response")
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("unexpected EOF: %v", err)
	}
}
