package sing_vless

import (
	"io"
	"net"
	"testing"

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
