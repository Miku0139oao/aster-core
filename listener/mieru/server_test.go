package mieru

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"

	mieruconstant "github.com/enfein/mieru/v3/apis/constant"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
)

// userConn is a net.Conn that also implements mieru's UserContext interface.
type userConn struct {
	net.Conn
	user string
}

func (c *userConn) UserName() string { return c.user }

// recordingTunnel captures the metadata handed to the tunnel.
type recordingTunnel struct {
	tcpMetadata chan *C.Metadata
	udpMetadata chan *C.Metadata
}

func newRecordingTunnel() *recordingTunnel {
	return &recordingTunnel{
		tcpMetadata: make(chan *C.Metadata, 1),
		udpMetadata: make(chan *C.Metadata, 1),
	}
}

func (t *recordingTunnel) HandleTCPConn(conn net.Conn, metadata *C.Metadata) {
	t.tcpMetadata <- metadata
	_ = conn.Close()
}

func (t *recordingTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	t.udpMetadata <- metadata
	packet.Drop()
}

func (*recordingTunnel) NatTable() C.NatTable { return nil }

func newPipePair(t *testing.T) (client *userConn, server *userConn) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() {
		_ = clientSide.Close()
		_ = serverSide.Close()
	})
	return &userConn{Conn: clientSide, user: "alice"}, &userConn{Conn: serverSide, user: "alice"}
}

// TestHandleTCPAppliesInUser checks that the mieru user name is propagated as
// the inbound user (and, for backwards compatibility, the inbound name).
func TestHandleTCPAppliesInUser(t *testing.T) {
	client, server := newPipePair(t)
	tunnel := newRecordingTunnel()

	request := &mierumodel.Request{
		Command: mieruconstant.Socks5ConnectCmd,
		DstAddr: mierumodel.AddrSpec{IP: net.IPv4(1, 2, 3, 4), Port: 443},
	}
	go Handle(server, tunnel, request)

	// Drain the fake socks5 response written by Handle.
	reply := make([]byte, 10)
	if err := readFullDeadline(client, reply, time.Second); err != nil {
		t.Fatalf("failed to read socks5 reply: %v", err)
	}

	select {
	case metadata := <-tunnel.tcpMetadata:
		if metadata.InUser != "alice" {
			t.Fatalf("metadata.InUser = %q, want %q", metadata.InUser, "alice")
		}
		if metadata.InName != "alice" {
			t.Fatalf("metadata.InName = %q, want %q", metadata.InName, "alice")
		}
	case <-time.After(time.Second):
		t.Fatal("Handle did not hand the TCP conn to the tunnel")
	}
}

func TestHandleUDPAppliesInUser(t *testing.T) {
	client, server := newPipePair(t)
	tunnel := newRecordingTunnel()

	request := &mierumodel.Request{
		Command: mieruconstant.Socks5UDPAssociateCmd,
		DstAddr: mierumodel.AddrSpec{IP: net.IPv4(1, 2, 3, 4), Port: 443},
	}
	go Handle(server, tunnel, request)

	// Read the fake socks5 response written by Handle.
	reply := make([]byte, 10)
	if err := readFullDeadline(client, reply, time.Second); err != nil {
		t.Fatalf("failed to read socks5 reply: %v", err)
	}

	// Write one packet-over-stream framed socks5 UDP packet.
	packet := bytes.Join([][]byte{
		{0, 0, 0, 1, 1, 2, 3, 4, 0, 53}, // socks5 UDP header for 1.2.3.4:53
		[]byte("payload"),
	}, nil)
	framed := bytes.Join([][]byte{
		{0x00, 0x00, byte(len(packet))},
		packet,
		{0xff},
	}, nil)
	if _, err := client.Write(framed); err != nil {
		t.Fatalf("failed to write packet: %v", err)
	}

	select {
	case metadata := <-tunnel.udpMetadata:
		if metadata.InUser != "alice" {
			t.Fatalf("metadata.InUser = %q, want %q", metadata.InUser, "alice")
		}
	case <-time.After(time.Second):
		t.Fatal("Handle did not hand the UDP packet to the tunnel")
	}
}

// TestHandleUDPClosesEndpoint guards the missing ep.Close(): when the packet
// loop exits the underlying stream must be closed, otherwise the endpoint leaks.
func TestHandleUDPClosesEndpoint(t *testing.T) {
	client, server := newPipePair(t)
	tunnel := newRecordingTunnel()

	request := &mierumodel.Request{
		Command: mieruconstant.Socks5UDPAssociateCmd,
		DstAddr: mierumodel.AddrSpec{IP: net.IPv4(1, 2, 3, 4), Port: 443},
	}

	closed := make(chan struct{})
	var once sync.Once
	server.Conn = &closeNotifyConn{Conn: server.Conn, onClose: func() { once.Do(func() { close(closed) }) }}

	done := make(chan struct{})
	go func() {
		Handle(server, tunnel, request)
		close(done)
	}()

	// Drain the fake socks5 response, then close the stream so the packet loop
	// observes EOF and Handle returns.
	reply := make([]byte, 10)
	if err := readFullDeadline(client, reply, time.Second); err != nil {
		t.Fatalf("failed to read socks5 reply: %v", err)
	}
	_ = client.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle did not return after the stream was closed")
	}

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("UDP endpoint was not closed")
	}
}

type closeNotifyConn struct {
	net.Conn
	onClose func()
}

func (c *closeNotifyConn) Close() error {
	c.onClose()
	return c.Conn.Close()
}

func readFullDeadline(conn net.Conn, buf []byte, timeout time.Duration) error {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, err := io.ReadFull(conn, buf)
	return err
}
