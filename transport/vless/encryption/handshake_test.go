package encryption

import (
	"bytes"
	"encoding/base64"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

const handshakeTestPadding = "100-35-35"

func x25519NFSKeys(t *testing.T) (priv, pub []byte) {
	t.Helper()
	privB64, pubB64, _, err := GenX25519("")
	if err != nil {
		t.Fatal(err)
	}
	priv, err = base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		t.Fatal(err)
	}
	pub, err = base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func newHandshakePair(t *testing.T, xorMode uint32, clientSeconds uint32, serverFrom, serverTo int64) (*ClientInstance, *ServerInstance) {
	t.Helper()
	priv, pub := x25519NFSKeys(t)
	server := &ServerInstance{}
	if err := server.Init([][]byte{priv}, xorMode, serverFrom, serverTo, handshakeTestPadding); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client := &ClientInstance{}
	if err := client.Init([][]byte{pub}, xorMode, clientSeconds, handshakeTestPadding); err != nil {
		t.Fatal(err)
	}
	return client, server
}

func plantClientTicket(client *ClientInstance) {
	client.RWLock.Lock()
	client.Expire = time.Now().Add(time.Hour)
	client.PfsKey = make([]byte, 64)
	client.Ticket = make([]byte, 16)
	for i := range client.PfsKey {
		client.PfsKey[i] = byte(i + 1)
	}
	for i := range client.Ticket {
		client.Ticket[i] = byte(i + 17)
	}
	client.RWLock.Unlock()
}

func expected0RTTPreWriteLen(client *ClientInstance) int {
	return 16 + client.RelaysLength + 18 + 32
}

func TestHandshake0RTTPreWriteExactCapacity(t *testing.T) {
	for _, xorMode := range []uint32{0, 1, 2} {
		xorMode := xorMode
		t.Run(xorModeName(xorMode), func(t *testing.T) {
			client, _ := newHandshakePair(t, xorMode, 1, 0, 0)
			plantClientTicket(client)
			conn, err := client.Handshake(discardConn{})
			if err != nil {
				t.Fatal(err)
			}
			want := expected0RTTPreWriteLen(client)
			if conn.PreWrite == nil {
				t.Fatal("0-RTT handshake returned nil PreWrite")
			}
			if len(conn.PreWrite) != want {
				t.Fatalf("PreWrite len=%d want=%d", len(conn.PreWrite), want)
			}
			if cap(conn.PreWrite) != len(conn.PreWrite) {
				t.Fatalf("PreWrite retained extra backing cap=%d len=%d", cap(conn.PreWrite), len(conn.PreWrite))
			}
			if xorMode == 2 {
				xorConn, ok := conn.Conn.(*XorConn)
				if !ok {
					t.Fatal("XorMode 2 did not wrap XorConn")
				}
				if xorConn.OutSkip != len(conn.PreWrite) {
					t.Fatalf("XorConn OutSkip=%d want %d", xorConn.OutSkip, len(conn.PreWrite))
				}
				if xorConn.InSkip != 16 {
					t.Fatalf("XorConn InSkip=%d want 16", xorConn.InSkip)
				}
			} else if _, ok := conn.Conn.(*XorConn); ok {
				t.Fatal("XorMode 0/1 unexpectedly wrapped XorConn")
			}
		})
	}
}

func TestHandshakeExpiredTicketFallsBackTo1RTT(t *testing.T) {
	client, _ := newHandshakePair(t, 0, 1, 0, 0)
	plantClientTicket(client)
	client.RWLock.Lock()
	client.Expire = time.Now().Add(-time.Second)
	client.RWLock.Unlock()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = a.SetDeadline(time.Now().Add(2 * time.Second))
	_ = b.SetDeadline(time.Now().Add(2 * time.Second))

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Handshake(a)
		errCh <- err
	}()
	buf := make([]byte, 1)
	if _, err := b.Read(buf); err != nil {
		t.Fatalf("expired ticket did not send 1-RTT client hello: %v", err)
	}
	_ = b.Close()
	err := <-errCh
	if err == nil {
		t.Fatal("expired-ticket 1-RTT handshake unexpectedly succeeded")
	}
}

func TestHandshake0RTTRoundTrip(t *testing.T) {
	payload := []byte("vless-0rtt-payload")
	for _, xorMode := range []uint32{0, 1, 2} {
		xorMode := xorMode
		t.Run(xorModeName(xorMode), func(t *testing.T) {
			client, server := newHandshakePair(t, xorMode, 1, 60, 60)
			if err := complete1RTT(t, client, server); err != nil {
				t.Fatal(err)
			}
			if time.Now().After(client.Expire) || len(client.Ticket) != 16 || len(client.PfsKey) != 64 {
				t.Fatalf("1-RTT did not store a ticket: expire=%v ticket=%d pfs=%d", client.Expire, len(client.Ticket), len(client.PfsKey))
			}

			cPipe, sPipe := bufferedPipe()
			defer cPipe.Close()
			defer sPipe.Close()
			deadline := time.Now().Add(5 * time.Second)
			_ = cPipe.SetDeadline(deadline)
			_ = sPipe.SetDeadline(deadline)

			clientConn, err := client.Handshake(cPipe)
			if err != nil {
				t.Fatal(err)
			}
			want := expected0RTTPreWriteLen(client)
			if cap(clientConn.PreWrite) != len(clientConn.PreWrite) || len(clientConn.PreWrite) != want {
				t.Fatalf("0-RTT PreWrite len=%d cap=%d want len=cap=%d", len(clientConn.PreWrite), cap(clientConn.PreWrite), want)
			}

			serverErr := make(chan error, 1)
			var serverConn *CommonConn
			go func() {
				sc, err := server.Handshake(sPipe, nil)
				serverConn = sc
				serverErr <- err
			}()
			if _, err := clientConn.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(serverConn, got); err != nil {
				t.Fatal(err)
			}
			if string(got) != string(payload) {
				t.Fatalf("server read %q want %q", got, payload)
			}
			if clientConn.PreWrite != nil {
				t.Fatal("first write did not consume PreWrite")
			}
		})
	}
}

func complete1RTT(t *testing.T, client *ClientInstance, server *ServerInstance) error {
	t.Helper()
	cPipe, sPipe := bufferedPipe()
	defer cPipe.Close()
	defer sPipe.Close()
	deadline := time.Now().Add(5 * time.Second)
	_ = cPipe.SetDeadline(deadline)
	_ = sPipe.SetDeadline(deadline)

	errCh := make(chan error, 2)
	go func() {
		_, err := server.Handshake(sPipe, nil)
		errCh <- err
	}()
	_, err := client.Handshake(cPipe)
	if err != nil {
		return err
	}
	return <-errCh
}

// bufferedPipe is an in-memory full-duplex conn pair with kernel-like buffers so
// 1-RTT/0-RTT handshakes can write a full hello while the peer writes a response.
type bufferedPipeConn struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	wait     sync.Cond
	closed   bool
	deadline time.Time
	peer     *bufferedPipeConn
}

func bufferedPipe() (net.Conn, net.Conn) {
	a := &bufferedPipeConn{}
	b := &bufferedPipeConn{}
	a.wait.L = &a.mu
	b.wait.L = &b.mu
	a.peer, b.peer = b, a
	return a, b
}

func (c *bufferedPipeConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.deadline.IsZero() {
		delay := time.Until(c.deadline)
		if delay <= 0 {
			return 0, osTimeoutError{}
		}
		timer := time.AfterFunc(delay, func() {
			c.mu.Lock()
			c.wait.Broadcast()
			c.mu.Unlock()
		})
		defer timer.Stop()
	}
	for c.buf.Len() == 0 {
		if c.closed {
			return 0, io.EOF
		}
		if err := c.deadlineErrLocked(); err != nil {
			return 0, err
		}
		c.wait.Wait()
	}
	return c.buf.Read(p)
}

func (c *bufferedPipeConn) Write(p []byte) (int, error) {
	peer := c.peer
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := peer.buf.Write(p)
	peer.wait.Broadcast()
	return n, err
}

func (c *bufferedPipeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.wait.Broadcast()
	c.mu.Unlock()
	c.peer.mu.Lock()
	c.peer.closed = true
	c.peer.wait.Broadcast()
	c.peer.mu.Unlock()
	return nil
}

func (c *bufferedPipeConn) LocalAddr() net.Addr  { return pipeAddr("local") }
func (c *bufferedPipeConn) RemoteAddr() net.Addr { return pipeAddr("remote") }

func (c *bufferedPipeConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	c.wait.Broadcast()
	return nil
}
func (c *bufferedPipeConn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *bufferedPipeConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

func (c *bufferedPipeConn) deadlineErrLocked() error {
	if c.deadline.IsZero() || time.Now().Before(c.deadline) {
		return nil
	}
	return osTimeoutError{}
}

type osTimeoutError struct{}

func (osTimeoutError) Error() string   { return "i/o timeout" }
func (osTimeoutError) Timeout() bool   { return true }
func (osTimeoutError) Temporary() bool { return true }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func xorModeName(mode uint32) string {
	switch mode {
	case 0:
		return "native"
	case 1:
		return "xorpub"
	case 2:
		return "random"
	default:
		return "unknown"
	}
}

func BenchmarkHandshake0RTTPreWrite(b *testing.B) {
	privB64, pubB64, _, err := GenX25519("")
	if err != nil {
		b.Fatal(err)
	}
	pub, err := base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil {
		b.Fatal(err)
	}
	_ = privB64
	client := &ClientInstance{}
	if err := client.Init([][]byte{pub}, 0, 1, handshakeTestPadding); err != nil {
		b.Fatal(err)
	}
	plantClientTicket(client)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := client.Handshake(discardConn{})
		if err != nil {
			b.Fatal(err)
		}
		if len(conn.PreWrite) == 0 {
			b.Fatal("0-RTT PreWrite empty")
		}
	}
}
