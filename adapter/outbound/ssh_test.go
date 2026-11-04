package outbound

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/ssh"
)

// writeFailConn is a net.Conn whose writes can be switched to fail on demand,
// emulating an underlying transport that breaks while reads keep working. That
// is exactly the state in which metacubex/ssh caches the write error and leaves
// Wait blocked.
type writeFailConn struct {
	net.Conn
	failWrite atomic.Bool
	closed    atomic.Bool
}

func (c *writeFailConn) Write(p []byte) (int, error) {
	if c.failWrite.Load() {
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}

func (c *writeFailConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func TestCloseOnWriteErrorConn(t *testing.T) {
	// success path must not close the connection
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	pipe := &writeFailConn{Conn: clientConn}
	wrapped := closeOnWriteErrorConn{Conn: pipe}
	go func() {
		buf := make([]byte, 4)
		_, _ = serverConn.Read(buf)
	}()
	if _, err := wrapped.Write([]byte("ping")); err != nil {
		t.Fatalf("write on healthy conn: %v", err)
	}
	if pipe.closed.Load() {
		t.Fatal("healthy write must not close the connection")
	}

	// error path must close the connection
	pipe.failWrite.Store(true)
	if _, err := wrapped.Write([]byte("ping")); err == nil {
		t.Fatal("expected the write to fail")
	}
	if !pipe.closed.Load() {
		t.Fatal("write error did not close the underlying connection")
	}
}

// TestSshConnectClosesTransportOnWriteError checks that the client returned by
// connect() force-closes the underlying connection when a packet write fails,
// so the mux/read loops can exit instead of wedging the shared client.
func TestSshConnectClosesTransportOnWriteError(t *testing.T) {
	// A real loopback socket is used because net.Pipe is unbuffered and would
	// deadlock while both peers write their version banner.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostKey, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(hostKey)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		serverSide, err := listener.Accept()
		if err != nil {
			return
		}
		defer serverSide.Close()
		server, chans, reqs, err := ssh.NewServerConn(serverSide, serverConfig)
		if err != nil {
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(reqs)
		for newChannel := range chans {
			_ = newChannel.Reject(ssh.Prohibited, "test")
		}
	}()

	var controlled *writeFailConn
	s, err := NewSsh(SshOption{
		BasicOption: BasicOption{
			DialerForAPI: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
				raw, err := net.Dial("tcp", listener.Addr().String())
				if err != nil {
					return nil, err
				}
				controlled = &writeFailConn{Conn: raw}
				return controlled, nil
			}),
		},
		Name:     "test",
		Server:   "127.0.0.1",
		Port:     22,
		UserName: "test",
		Password: "test",
	})
	if err != nil {
		t.Fatalf("NewSsh: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := s.connect(ctx, s.addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// Break the write direction only; reads stay healthy, reproducing the state
	// where ssh caches the write error without closing the transport.
	controlled.failWrite.Store(true)
	// Failing to open a channel is expected. Without the wrapper the write
	// error is cached and the call can block, so bound it to keep the failure
	// mode a clean test failure instead of a hung test binary.
	dialDone := make(chan struct{})
	go func() {
		defer close(dialDone)
		_, _ = client.Dial("tcp", "127.0.0.1:1")
	}()
	select {
	case <-dialDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ssh channel dial hung after the transport write error")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !controlled.closed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("write error did not close the underlying ssh connection")
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
	}
}
