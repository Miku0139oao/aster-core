package adapter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	N "github.com/Miku0139oao/aster-core/common/net"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type gostWSDialerFunc func(ctx context.Context, network, address string) (net.Conn, error)

func (f gostWSDialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func (f gostWSDialerFunc) ListenPacket(context.Context, string, string, netip.AddrPort) (net.PacketConn, error) {
	return nil, C.ErrNotSupport
}

type gostWSUpgrade struct {
	path    string
	host    string
	headers http.Header
	relay   []byte
	err     error
}

// serveFakeGostWS performs the server side of a GOST WebSocket relay handshake
// over conn, then returns the HTTP upgrade details and the relay CONNECT
// request the client sent inside the WebSocket frames. reply is written back as
// a single WebSocket message once the relay request has been consumed.
func serveFakeGostWS(conn net.Conn, reply []byte) <-chan gostWSUpgrade {
	out := make(chan gostWSUpgrade, 1)
	go func() {
		defer conn.Close()
		var observed gostWSUpgrade

		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			observed.err = fmt.Errorf("read upgrade request: %w", err)
			out <- observed
			return
		}
		observed.path = req.URL.Path
		observed.host = req.Host
		observed.headers = req.Header.Clone()

		accept := N.GetWebSocketSecAccept(req.Header.Get("Sec-WebSocket-Key"))
		if _, err := fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
			observed.err = fmt.Errorf("write upgrade response: %w", err)
			out <- observed
			return
		}

		reader := wsutil.NewReader(br, ws.StateServerSide)
		needed := 4 // relay header: version, command, feature length
		var relay []byte
		for len(relay) < needed {
			header, err := reader.NextFrame()
			if err != nil {
				observed.err = fmt.Errorf("read relay frame: %w", err)
				out <- observed
				return
			}
			if header.OpCode == ws.OpClose {
				out <- observed
				return
			}
			if header.OpCode != ws.OpBinary && header.OpCode != ws.OpText {
				continue
			}
			payload := make([]byte, header.Length)
			if _, err := io.ReadFull(reader, payload); err != nil {
				observed.err = fmt.Errorf("read relay payload: %w", err)
				out <- observed
				return
			}
			relay = append(relay, payload...)
			// Once the relay header is complete, wait for the advertised
			// feature list too so the client can finish writing before the
			// reply unblocks it on the synchronous net.Pipe.
			if needed == 4 && len(relay) >= 4 {
				needed = 4 + (int(relay[2])<<8 | int(relay[3]))
			}
		}
		observed.relay = relay

		if err := wsutil.WriteServerMessage(conn, ws.OpBinary, reply); err != nil {
			observed.err = fmt.Errorf("write relay reply: %w", err)
		}
		out <- observed
	}()
	return out
}

// TestGostRelayWebsocketOptions parses a gost-relay proxy with ws/ws-opts and
// checks that the option reaches the transport: the dialer must perform a
// WebSocket upgrade on the configured path with the configured headers, and
// the deferred relay CONNECT response must be stripped from the first read.
func TestGostRelayWebsocketOptions(t *testing.T) {
	// A successful CONNECT response (version 1, status 0, no features)
	// followed by the first bytes of relayed payload.
	relayReply := append([]byte{0x01, 0x00, 0x00, 0x00}, []byte("hello")...)

	testCases := []struct {
		name       string
		wsOpts     map[string]any
		wantPath   string
		wantHost   string
		wantHeader map[string]string
	}{
		{
			name:     "default path",
			wantPath: "/ws",
			wantHost: "relay.example.com",
		},
		{
			name: "custom path and headers",
			wsOpts: map[string]any{
				"path": "/custom-ws",
				"headers": map[string]any{
					"Host":   "ws.example.com",
					"X-Test": "aster",
				},
			},
			wantPath:   "/custom-ws",
			wantHost:   "ws.example.com",
			wantHeader: map[string]string{"X-Test": "aster"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			t.Cleanup(func() {
				_ = clientConn.Close()
				_ = serverConn.Close()
			})
			upgrades := serveFakeGostWS(serverConn, relayReply)

			mapping := map[string]any{
				"name":   "gost-ws",
				"type":   "gost-relay",
				"server": "relay.example.com",
				"port":   443,
				"ws":     true,
			}
			if testCase.wsOpts != nil {
				mapping["ws-opts"] = testCase.wsOpts
			}

			proxy, err := ParseProxy(mapping, WithDialerForAPI(gostWSDialerFunc(
				func(context.Context, string, string) (net.Conn, error) {
					return clientConn, nil
				},
			)))
			if err != nil {
				t.Fatalf("ParseProxy: %v", err)
			}
			defer proxy.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := proxy.DialContext(ctx, &C.Metadata{
				NetWork: C.TCP,
				Type:    C.SOCKS5,
				Host:    "target.example.com",
				DstPort: 80,
			})
			if err != nil {
				t.Fatalf("DialContext: %v", err)
			}
			defer conn.Close()

			payload := make([]byte, len("hello"))
			if _, err := io.ReadFull(conn, payload); err != nil {
				t.Fatalf("read relayed payload: %v", err)
			}
			if got, want := string(payload), "hello"; got != want {
				t.Fatalf("relayed payload = %q, want %q", got, want)
			}

			select {
			case observed := <-upgrades:
				if observed.err != nil {
					t.Fatalf("fake server: %v", observed.err)
				}
				if observed.path != testCase.wantPath {
					t.Errorf("upgrade path = %q, want %q", observed.path, testCase.wantPath)
				}
				if observed.host != testCase.wantHost {
					t.Errorf("upgrade host = %q, want %q", observed.host, testCase.wantHost)
				}
				for key, want := range testCase.wantHeader {
					if got := observed.headers.Get(key); got != want {
						t.Errorf("upgrade header %s = %q, want %q", key, got, want)
					}
				}
				if len(observed.relay) < 2 || observed.relay[0] != 0x01 || observed.relay[1] != 0x01 {
					t.Errorf("relay request header = %x, want version 1 connect command", observed.relay)
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for the WebSocket upgrade request")
			}
		})
	}
}
