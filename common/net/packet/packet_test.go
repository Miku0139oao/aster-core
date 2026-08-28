package packet

import (
	"net"
	"sync"
	"testing"
	"time"
)

type stubPacketConn struct {
	payload []byte
	addr    net.Addr
}

func (c *stubPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n := copy(p, c.payload)
	return n, c.addr, nil
}

func (c *stubPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) { return len(p), nil }
func (c *stubPacketConn) Close() error                              { return nil }
func (c *stubPacketConn) LocalAddr() net.Addr                       { return c.addr }
func (c *stubPacketConn) SetDeadline(time.Time) error               { return nil }
func (c *stubPacketConn) SetReadDeadline(time.Time) error           { return nil }
func (c *stubPacketConn) SetWriteDeadline(time.Time) error          { return nil }

func TestWaitReadFromGenericCopiesPayload(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 53}
	pc := NewEnhancePacketConn(&stubPacketConn{
		payload: []byte("generic-packet"),
		addr:    addr,
	})
	data, put, gotAddr, err := pc.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put == nil {
		t.Fatal("expected put func")
	}
	defer put()
	if string(data) != "generic-packet" {
		t.Fatalf("payload = %q", data)
	}
	if gotAddr == nil || gotAddr.String() != addr.String() {
		t.Fatalf("addr = %v, want %v", gotAddr, addr)
	}
}

func TestEnhanceUDPConnWaitReadFrom(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	payload := []byte("udp-wait-read")
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}

	conn := NewEnhancePacketConn(server)
	data, put, addr, err := conn.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put != nil {
		defer put()
	}
	if string(data) != string(payload) {
		t.Fatalf("payload = %q, want %q", data, payload)
	}
	if addr == nil {
		t.Fatal("nil addr")
	}
}

func TestWaitReadFromGenericPutIdempotent(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 53}
	pc := NewEnhancePacketConn(&stubPacketConn{
		payload: []byte("generic-packet"),
		addr:    addr,
	})
	for i := 0; i < 100; i++ {
		data, put, _, err := pc.WaitReadFrom()
		if err != nil {
			t.Fatal(err)
		}
		if put == nil {
			t.Fatal("expected put func")
		}
		if len(data) == 0 {
			t.Fatal("empty payload")
		}
		put()
		put() // must not double-Put into the slot pool
	}
}

func TestWaitReadFromGenericConcurrentPut(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 53}
	pc := NewEnhancePacketConn(&stubPacketConn{
		payload: []byte("generic-packet"),
		addr:    addr,
	})
	_, put, _, err := pc.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put == nil {
		t.Fatal("expected put func")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		put()
	}()
	go func() {
		defer wg.Done()
		put()
	}()
	wg.Wait()

	data, put, _, err := pc.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put == nil {
		t.Fatal("expected put func after concurrent put")
	}
	defer put()
	if string(data) != "generic-packet" {
		t.Fatalf("payload = %q", data)
	}
}

func BenchmarkWaitReadFromGeneric(b *testing.B) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 53}
	pc := NewEnhancePacketConn(&stubPacketConn{
		payload: []byte("generic-packet"),
		addr:    addr,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, put, gotAddr, err := pc.WaitReadFrom()
		if err != nil {
			b.Fatal(err)
		}
		if put != nil {
			put()
		}
		if gotAddr == nil || len(data) == 0 {
			b.Fatal("empty read")
		}
	}
}

func BenchmarkEnhanceUDPConnWaitReadFrom(b *testing.B) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		b.Fatal(err)
	}
	defer server.Close()

	client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	payload := []byte("bench-udp-payload")
	conn := NewEnhancePacketConn(server)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
		data, put, addr, err := conn.WaitReadFrom()
		if err != nil {
			b.Fatal(err)
		}
		if put != nil {
			put()
		}
		if addr == nil || len(data) != len(payload) {
			b.Fatalf("bad read len=%d addr=%v", len(data), addr)
		}
	}
}
