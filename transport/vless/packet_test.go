package vless

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type gatedWriteConn struct {
	net.Conn
	calls         atomic.Int32
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
	mu            sync.Mutex
	data          []byte
}

func newGatedWriteConn() *gatedWriteConn {
	return &gatedWriteConn{
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
}

func (c *gatedWriteConn) Write(data []byte) (int, error) {
	call := c.calls.Add(1)
	if call == 1 {
		close(c.firstEntered)
		<-c.releaseFirst
	} else if call == 2 {
		close(c.secondEntered)
	}
	c.mu.Lock()
	c.data = append(c.data, data...)
	c.mu.Unlock()
	return len(data), nil
}

type gatedReadConn struct {
	net.Conn
	calls       atomic.Int32
	secondRead  chan struct{}
	thirdRead   chan struct{}
	releaseBody chan struct{}
	mu          sync.Mutex
	data        []byte
	offset      int
}

func newGatedReadConn(data []byte) *gatedReadConn {
	return &gatedReadConn{
		secondRead:  make(chan struct{}),
		thirdRead:   make(chan struct{}),
		releaseBody: make(chan struct{}),
		data:        data,
	}
}

func (c *gatedReadConn) Read(buffer []byte) (int, error) {
	call := c.calls.Add(1)
	if call == 2 {
		close(c.secondRead)
		<-c.releaseBody
	} else if call == 3 {
		close(c.thirdRead)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.offset == len(c.data) {
		return 0, io.EOF
	}
	n := copy(buffer, c.data[c.offset:])
	c.offset += n
	return n, nil
}

func TestPacketConnReadFromDrainsShortBufferFrame(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	require.NoError(t, client.SetDeadline(time.Now().Add(time.Second)))
	packetConn := &PacketConn{Conn: client, rAddr: server.LocalAddr()}

	writeDone := make(chan error, 1)
	go func() {
		_, err := server.Write([]byte{0, 4, 'A', 'B', 'C', 'D', 0, 1, 'Z'})
		writeDone <- err
	}()

	n, _, err := packetConn.ReadFrom(make([]byte, 2))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.ErrShortBuffer)

	buffer := make([]byte, 1)
	n, _, err = packetConn.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []byte{'Z'}, buffer)
	require.NoError(t, <-writeDone)
}

func TestPacketConnWriteToRejectsOversizedPacket(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	packetConn := &PacketConn{Conn: client}

	n, err := packetConn.WriteTo(make([]byte, 65536), nil)
	require.Zero(t, n)
	require.Error(t, err)
	require.False(t, errors.Is(err, net.ErrClosed))
}

func TestPacketConnSerializesConcurrentWrites(t *testing.T) {
	conn := newGatedWriteConn()
	packetConn := &PacketConn{Conn: conn}
	results := make(chan error, 2)
	go func() {
		_, err := packetConn.WriteTo([]byte("one"), nil)
		results <- err
	}()
	<-conn.firstEntered

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := packetConn.WriteTo([]byte("two"), nil)
		results <- err
	}()
	<-secondStarted

	var enteredBeforeRelease bool
	select {
	case <-conn.secondEntered:
		enteredBeforeRelease = true
	case <-time.After(50 * time.Millisecond):
	}
	close(conn.releaseFirst)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.False(t, enteredBeforeRelease)
	require.Equal(t, []byte{0, 3, 'o', 'n', 'e', 0, 3, 't', 'w', 'o'}, conn.data)
}

func TestPacketConnSerializesConcurrentReads(t *testing.T) {
	conn := newGatedReadConn([]byte{0, 3, 'o', 'n', 'e', 0, 3, 't', 'w', 'o'})
	packetConn := &PacketConn{Conn: conn}
	type result struct {
		data string
		err  error
	}
	results := make(chan result, 2)
	read := func() {
		buffer := make([]byte, 3)
		n, _, err := packetConn.ReadFrom(buffer)
		results <- result{data: string(buffer[:n]), err: err}
	}
	go read()
	<-conn.secondRead

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		read()
	}()
	<-secondStarted

	var enteredBeforeRelease bool
	select {
	case <-conn.thirdRead:
		enteredBeforeRelease = true
	case <-time.After(50 * time.Millisecond):
	}
	close(conn.releaseBody)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.False(t, enteredBeforeRelease)
	require.ElementsMatch(t, []string{"one", "two"}, []string{first.data, second.data})
}

type discardPacketConn struct {
	net.Conn
}

func (discardPacketConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardPacketConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (discardPacketConn) Close() error                { return nil }

type replayPacketConn struct {
	net.Conn
	frame  []byte
	offset int
}

func (c *replayPacketConn) Read(p []byte) (int, error) {
	if c.offset >= len(c.frame) {
		c.offset = 0
	}
	n := copy(p, c.frame[c.offset:])
	c.offset += n
	return n, nil
}

func (c *replayPacketConn) Close() error { return nil }

func BenchmarkPacketConnWriteTo(b *testing.B) {
	payload := make([]byte, 512)
	packetConn := &PacketConn{Conn: discardPacketConn{}}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := packetConn.WriteTo(payload, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPacketConnReadFrom(b *testing.B) {
	payload := make([]byte, 512)
	frame := make([]byte, 2+len(payload))
	frame[0] = byte(len(payload) >> 8)
	frame[1] = byte(len(payload))
	copy(frame[2:], payload)
	packetConn := &PacketConn{Conn: &replayPacketConn{frame: frame}}
	out := make([]byte, len(payload))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, _, err := packetConn.ReadFrom(out)
		if err != nil {
			b.Fatal(err)
		}
		if n != len(payload) {
			b.Fatalf("n=%d", n)
		}
	}
}
