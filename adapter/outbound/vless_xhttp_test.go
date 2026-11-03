package outbound

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
)

// dialerFunc adapts a dial function to C.Dialer.
type dialerFunc func(ctx context.Context, network, address string) (net.Conn, error)

func (f dialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func (f dialerFunc) ListenPacket(context.Context, string, string, netip.AddrPort) (net.PacketConn, error) {
	return nil, C.ErrNotSupport
}

// TestVlessXHTTPBracketsIPv6Host reproduces a VLESS+xhttp outbound configured
// with a bare IPv6 server: the request URL was built from an unbracketed
// literal, so the request could not even be constructed.
func TestVlessXHTTPBracketsIPv6Host(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	requests := make(chan *http.Request, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err != nil {
			close(requests)
			return
		}
		requests <- req
		_, _ = serverConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
	}()

	option := VlessOption{
		BasicOption: BasicOption{
			DialerForAPI: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
				return clientConn, nil
			}),
		},
		Name:    "test",
		Server:  "2001:db8::1",
		Port:    443,
		UUID:    "b831381d-6324-4d53-ad4f-8cda48b3081c",
		Network: "xhttp",
		TLS:     false,
		ALPN:    []string{"http/1.1"},
		XHTTPOpts: XHTTPOptions{
			Host: "2001:db8::1",
			Mode: "stream-one",
		},
	}

	v, err := NewVless(option)
	if err != nil {
		t.Fatalf("NewVless: %v", err)
	}
	defer v.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := v.dialContext(dialCtx)
	if err != nil {
		t.Fatalf("dialContext: %v", err)
	}
	defer conn.Close()

	select {
	case req, ok := <-requests:
		if !ok {
			t.Fatal("server never received a request")
		}
		if req.Host != "[2001:db8::1]" {
			t.Fatalf("Host header = %q, want %q", req.Host, "[2001:db8::1]")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the xhttp request")
	}
}

// TestVlessXHTTPDownloadSettingsBracketsIPv6Host covers the same defect on the
// download-settings transport.
func TestVlessXHTTPDownloadSettingsBracketsIPv6Host(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	requests := make(chan *http.Request, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err != nil {
			close(requests)
			return
		}
		requests <- req
		_, _ = serverConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
	}()

	downloadHost := "2001:db8::2"
	downloadServer := "2001:db8::1"
	option := VlessOption{
		BasicOption: BasicOption{
			DialerForAPI: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
				return clientConn, nil
			}),
		},
		Name:    "test",
		Server:  "2001:db8::1",
		Port:    443,
		UUID:    "b831381d-6324-4d53-ad4f-8cda48b3081c",
		Network: "xhttp",
		TLS:     false,
		ALPN:    []string{"http/1.1"},
		XHTTPOpts: XHTTPOptions{
			Host: "2001:db8::1",
			Mode: "stream-up",
			DownloadSettings: &XHTTPDownloadSettings{
				Host:   &downloadHost,
				Server: &downloadServer,
			},
		},
	}

	v, err := NewVless(option)
	if err != nil {
		t.Fatalf("NewVless: %v", err)
	}
	defer v.Close()

	if got := v.xhttpClient; got == nil {
		t.Fatal("xhttp client was not created")
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := v.dialContext(dialCtx)
	if err != nil {
		t.Fatalf("dialContext: %v", err)
	}
	defer conn.Close()

	select {
	case req, ok := <-requests:
		if !ok {
			t.Fatal("server never received a request")
		}
		if req.Host != "[2001:db8::2]" {
			t.Fatalf("Host header = %q, want %q", req.Host, "[2001:db8::2]")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the xhttp request")
	}
}
