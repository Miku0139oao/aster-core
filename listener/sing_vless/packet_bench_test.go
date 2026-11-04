package sing_vless

import (
	"io"
	"net"
	"sync"
	"testing"

	"github.com/metacubex/sing/common/buf"
	"github.com/metacubex/sing/common/bufio"
	M "github.com/metacubex/sing/common/metadata"
)

type discardNetConn struct {
	net.Conn
}

func (discardNetConn) Write(p []byte) (int, error) { return len(p), nil }
func (discardNetConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (discardNetConn) Close() error                { return nil }

type replayNetConn struct {
	net.Conn
	frame  []byte
	offset int
}

func (c *replayNetConn) Read(p []byte) (int, error) {
	if c.offset >= len(c.frame) {
		c.offset = 0
	}
	n := copy(p, c.frame[c.offset:])
	c.offset += n
	return n, nil
}

func (c *replayNetConn) Close() error { return nil }

type captureNetConn struct {
	net.Conn
	data []byte
}

func (c *captureNetConn) Write(p []byte) (int, error) {
	c.data = append(c.data, p...)
	return len(p), nil
}

func (c *captureNetConn) Close() error { return nil }

func TestServerPacketConnWriteToLengthPrefix(t *testing.T) {
	capture := &captureNetConn{}
	conn := newServerPacketConn(bufio.NewExtendedConn(capture), M.ParseSocksaddr("192.0.2.1:443"))
	n, err := conn.WriteTo([]byte("abc"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
	want := []byte{0, 3, 'a', 'b', 'c'}
	if string(capture.data) != string(want) {
		t.Fatalf("got %v want %v", capture.data, want)
	}
}

func TestServerPacketConnReadFromReusesCachedAddr(t *testing.T) {
	frame := []byte{0, 3, 'x', 'y', 'z', 0, 3, 'x', 'y', 'z'}
	conn := newServerPacketConn(bufio.NewExtendedConn(&replayNetConn{frame: frame}), M.ParseSocksaddr("192.0.2.1:443"))
	first := make([]byte, 3)
	_, addr1, err := conn.ReadFrom(first)
	if err != nil {
		t.Fatal(err)
	}
	second := make([]byte, 3)
	_, addr2, err := conn.ReadFrom(second)
	if err != nil {
		t.Fatal(err)
	}
	if addr1 != addr2 {
		t.Fatalf("cached addr changed: %p vs %p", addr1, addr2)
	}
	if addr1 == nil {
		t.Fatal("expected cached UDP addr")
	}
}

func BenchmarkServerPacketConnWriteTo(b *testing.B) {
	payload := make([]byte, 512)
	conn := newServerPacketConn(bufio.NewExtendedConn(discardNetConn{}), M.ParseSocksaddr("192.0.2.1:443"))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.WriteTo(payload, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServerPacketConnReadFrom(b *testing.B) {
	payload := make([]byte, 512)
	frame := make([]byte, 2+len(payload))
	frame[0] = byte(len(payload) >> 8)
	frame[1] = byte(len(payload))
	copy(frame[2:], payload)
	conn := newServerPacketConn(bufio.NewExtendedConn(&replayNetConn{frame: frame}), M.ParseSocksaddr("192.0.2.1:443"))
	out := make([]byte, len(payload))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, _, err := conn.ReadFrom(out)
		if err != nil {
			b.Fatal(err)
		}
		if n != len(payload) {
			b.Fatalf("n=%d", n)
		}
	}
}

type countingNetConn struct {
	net.Conn
	writes int
	bytes  []byte
}

func (c *countingNetConn) Write(p []byte) (int, error) {
	c.writes++
	c.bytes = append(c.bytes, p...)
	return len(p), nil
}

func (c *countingNetConn) Close() error { return nil }

// TestServerPacketConnWriteToSingleWrite pins the reason WriteTo was rewritten:
// one datagram must reach the transport in one write, otherwise every drop
// costs two TLS records and two AEAD seals.
func TestServerPacketConnWriteToSingleWrite(t *testing.T) {
	counter := &countingNetConn{}
	conn := newServerPacketConn(bufio.NewExtendedConn(counter), M.ParseSocksaddr("192.0.2.1:443"))
	payload := []byte("hello-udp")
	n, err := conn.WriteTo(payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Fatalf("n=%d want %d", n, len(payload))
	}
	if counter.writes != 1 {
		t.Fatalf("writes=%d, want 1", counter.writes)
	}
	want := append([]byte{0, byte(len(payload))}, payload...)
	if string(counter.bytes) != string(want) {
		t.Fatalf("got %v want %v", counter.bytes, want)
	}
}

// TestServerPacketConnWriteToAfterWritePacket checks that WriteTo still emits
// exactly the wire format WritePacket does, including for a payload larger
// than one buffer would naturally hold in reserve.
func TestServerPacketConnWriteToLargePayload(t *testing.T) {
	for _, size := range []int{0, 1, 255, 256, 1400, 65000} {
		counter := &countingNetConn{}
		conn := newServerPacketConn(bufio.NewExtendedConn(counter), M.ParseSocksaddr("192.0.2.1:443"))
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}
		n, err := conn.WriteTo(payload, nil)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if n != size {
			t.Fatalf("size %d: n=%d", size, n)
		}
		if counter.writes != 1 {
			t.Fatalf("size %d: writes=%d, want 1", size, counter.writes)
		}
		if len(counter.bytes) != size+2 {
			t.Fatalf("size %d: got %d bytes, want %d", size, len(counter.bytes), size+2)
		}
		if got := int(counter.bytes[0])<<8 | int(counter.bytes[1]); got != size {
			t.Fatalf("size %d: prefix says %d", size, got)
		}
		if string(counter.bytes[2:]) != string(payload) {
			t.Fatalf("size %d: payload mismatch", size)
		}
	}
}

type isolatedBufferPool struct {
	pool sync.Pool
}

func (p *isolatedBufferPool) Get(size int) []byte {
	v := p.pool.Get()
	if v == nil {
		return make([]byte, size)
	}
	b := v.([]byte)
	if cap(b) < size {
		return make([]byte, size)
	}
	return b[:size]
}

func (p *isolatedBufferPool) Put(b []byte) error {
	if cap(b) >= 512 {
		p.pool.Put(b[:cap(b)]) //nolint:staticcheck
	}
	return nil
}

// BenchmarkServerPacketConnWriteToIsolated measures the single-write WriteTo
// against a private allocator. The shared sing allocator benchmark above
// includes buffer churn on multiple size classes and is not comparable across
// versions of this file.
func BenchmarkServerPacketConnWriteToIsolated(b *testing.B) {
	saved := buf.DefaultAllocator
	buf.DefaultAllocator = &isolatedBufferPool{}
	defer func() { buf.DefaultAllocator = saved }()

	payload := make([]byte, 512)
	conn := newServerPacketConn(bufio.NewExtendedConn(discardNetConn{}), M.ParseSocksaddr("192.0.2.1:443"))
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.WriteTo(payload, nil); err != nil {
			b.Fatal(err)
		}
	}
}
