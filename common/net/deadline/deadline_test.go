package deadline

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/net/packet"

	"github.com/metacubex/sing/common/buf"
	M "github.com/metacubex/sing/common/metadata"
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

type stubEnhancePacketConn struct {
	stubPacketConn
}

func (c *stubEnhancePacketConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	return c.payload, nil, c.addr, nil
}

func TestPipeDeadlineZeroDoesNotFire(t *testing.T) {
	d := MakePipeDeadline()
	select {
	case <-d.Wait():
		t.Fatal("zero deadline fired")
	default:
	}
}

func TestPipeDeadlinePastFires(t *testing.T) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(-time.Second))
	select {
	case <-d.Wait():
	default:
		t.Fatal("past deadline did not fire")
	}
}

func TestPipeDeadlineResetAfterFire(t *testing.T) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(-time.Millisecond))
	<-d.Wait()
	d.Set(time.Time{})
	select {
	case <-d.Wait():
		t.Fatal("reset zero deadline still closed")
	default:
	}
}

func TestPipeDeadlineConcurrentWaitSet(t *testing.T) {
	d := MakePipeDeadline()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			ch := d.Wait()
			if ch == nil {
				t.Error("nil wait channel")
				return
			}
			select {
			case <-ch:
			default:
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			switch i % 3 {
			case 0:
				d.Set(time.Time{})
			case 1:
				d.Set(time.Now().Add(time.Hour))
			default:
				d.Set(time.Now().Add(-time.Millisecond))
			}
		}
	}()
	wg.Wait()
}

func TestPipeDeadlineRefreshKeepsWaitChannel(t *testing.T) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(time.Hour))
	ch := d.Wait()
	if ch == nil {
		t.Fatal("nil wait channel")
	}
	for i := 0; i < 10; i++ {
		d.Set(time.Now().Add(time.Hour))
		if d.Wait() != ch {
			t.Fatalf("refresh %d changed Wait channel", i)
		}
		select {
		case <-ch:
			t.Fatal("unfired refresh woke Wait")
		default:
		}
	}
	if d.timer == nil {
		t.Fatal("live refresh dropped timer")
	}
	d.Set(time.Time{})
	if d.timer != nil {
		t.Fatal("Set(zero) after refresh left a timer")
	}
}

func TestPipeDeadlineFutureZeroFutureDoesNotHang(t *testing.T) {
	d := MakePipeDeadline()
	done := make(chan struct{})
	go func() {
		d.Set(time.Now().Add(time.Hour))
		d.Set(time.Time{})
		d.Set(time.Now().Add(time.Hour))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("future->zero->future hung")
	}
	select {
	case <-d.Wait():
		t.Fatal("future after zero already closed")
	default:
	}
	d.Set(time.Time{})
}

func TestPipeDeadlineFuturePastFuture(t *testing.T) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(time.Hour))
	old := d.Wait()
	d.Set(time.Now().Add(-time.Millisecond))
	if d.timer != nil {
		t.Fatal("Set(past) left a timer")
	}
	select {
	case <-old:
	default:
		t.Fatal("past did not close Wait channel")
	}
	d.Set(time.Now().Add(time.Hour))
	next := d.Wait()
	if next == old {
		t.Fatal("future after past reused closed channel")
	}
	select {
	case <-next:
		t.Fatal("future after past already closed")
	default:
	}
	d.Set(time.Time{})
}

func TestPipeDeadlineFiredThenFutureNewChannel(t *testing.T) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(15 * time.Millisecond))
	old := d.Wait()
	select {
	case <-old:
	case <-time.After(time.Second):
		t.Fatal("timer did not fire")
	}
	d.Set(time.Now().Add(time.Hour))
	next := d.Wait()
	if next == old {
		t.Fatal("future after fire reused closed channel")
	}
	select {
	case <-next:
		t.Fatal("future after fire already closed")
	default:
	}
	d.Set(time.Time{})
}

func TestPipeDeadlineRepeatedPastAndZero(t *testing.T) {
	d := MakePipeDeadline()
	for i := 0; i < 100; i++ {
		d.Set(time.Time{})
		if d.timer != nil {
			t.Fatalf("zero iteration %d left a timer", i)
		}
		d.Set(time.Now().Add(-time.Nanosecond))
		if d.timer != nil {
			t.Fatalf("past iteration %d left a timer", i)
		}
		select {
		case <-d.Wait():
		default:
			t.Fatalf("past iteration %d did not fire", i)
		}
	}
}

func TestNetPacketConnReadFromZeroDeadline(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}
	pc := NewNetPacketConn(&stubPacketConn{
		payload: []byte("zero-deadline"),
		addr:    addr,
	})
	buf := make([]byte, 64)
	n, gotAddr, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "zero-deadline" {
		t.Fatalf("payload = %q", buf[:n])
	}
	if gotAddr.String() != addr.String() {
		t.Fatalf("addr = %v, want %v", gotAddr, addr)
	}
}

func TestNetPacketConnReadFromDeadlinePipe(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}
	pc := NewNetPacketConn(&stubPacketConn{
		payload: []byte("pipe-deadline"),
		addr:    addr,
	})
	if err := pc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, gotAddr, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "pipe-deadline" {
		t.Fatalf("payload = %q", buf[:n])
	}
	if gotAddr.String() != addr.String() {
		t.Fatalf("addr = %v, want %v", gotAddr, addr)
	}
}

func TestNetPacketConnPastDeadline(t *testing.T) {
	pc := NewNetPacketConn(&stubPacketConn{
		payload: []byte("late"),
		addr:    &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9},
	})
	if err := pc.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, err := pc.ReadFrom(make([]byte, 16))
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("err = %v, want os.ErrDeadlineExceeded", err)
	}
}

func TestEnhanceWaitReadFromWithDeadline(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 53}
	pc := NewEnhancePacketConn(&stubEnhancePacketConn{stubPacketConn{
		payload: []byte("enhance-pipe"),
		addr:    addr,
	}})
	if err := pc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	data, put, gotAddr, err := pc.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put != nil {
		put()
	}
	if string(data) != "enhance-pipe" {
		t.Fatalf("payload = %q", data)
	}
	if gotAddr.String() != addr.String() {
		t.Fatalf("addr = %v, want %v", gotAddr, addr)
	}
}

func TestSingPacketWaitReadFromWithDeadline(t *testing.T) {
	stub := &singPacketConnStub{
		payload:     []byte("sing-enhance-deadline"),
		destination: M.ParseSocksaddr("198.51.100.2:53"),
	}
	conn := packet.NewEnhancePacketConn(stub)
	pc := NewEnhancePacketConn(conn)
	if err := pc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	data, put, addr, err := pc.WaitReadFrom()
	if err != nil {
		t.Fatal(err)
	}
	if put != nil {
		put()
	}
	if string(data) != string(stub.payload) {
		t.Fatalf("payload = %q", data)
	}
	if addr == nil {
		t.Fatal("nil addr")
	}
}

func BenchmarkPipeDeadlineWait(b *testing.B) {
	d := MakePipeDeadline()
	_ = d.Wait()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d.Wait() == nil {
			b.Fatal("nil wait channel")
		}
	}
}

func BenchmarkPipeDeadlineSetRefresh(b *testing.B) {
	d := MakePipeDeadline()
	d.Set(time.Now().Add(time.Hour))
	b.Cleanup(func() { d.Set(time.Time{}) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Set(time.Now().Add(time.Hour))
	}
}

func BenchmarkNetPacketConnReadFromZeroDeadline(b *testing.B) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}
	pc := NewNetPacketConn(&stubPacketConn{
		payload: []byte("zero-deadline"),
		addr:    addr,
	})
	buf := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, gotAddr, err := pc.ReadFrom(buf)
		if err != nil || n == 0 || gotAddr == nil {
			b.Fatalf("n=%d addr=%v err=%v", n, gotAddr, err)
		}
	}
}

func BenchmarkNetPacketConnReadFromWithDeadline(b *testing.B) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}
	pc := NewNetPacketConn(&stubPacketConn{
		payload: []byte("pipe-deadline"),
		addr:    addr,
	})
	if err := pc.SetReadDeadline(time.Now().Add(time.Hour)); err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, gotAddr, err := pc.ReadFrom(buf)
		if err != nil || n == 0 || gotAddr == nil {
			b.Fatalf("n=%d addr=%v err=%v", n, gotAddr, err)
		}
	}
}

func BenchmarkEnhanceWaitReadFromZeroDeadline(b *testing.B) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 53}
	pc := NewEnhancePacketConn(&stubEnhancePacketConn{stubPacketConn{
		payload: []byte("enhance-zero"),
		addr:    addr,
	}})
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

func BenchmarkEnhanceWaitReadFromWithDeadline(b *testing.B) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 53}
	pc := NewEnhancePacketConn(&stubEnhancePacketConn{stubPacketConn{
		payload: []byte("enhance-pipe"),
		addr:    addr,
	}})
	if err := pc.SetReadDeadline(time.Now().Add(time.Hour)); err != nil {
		b.Fatal(err)
	}
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

func BenchmarkSingPacketReadWithDeadline(b *testing.B) {
	stub := &singPacketConnStub{
		payload:     []byte("deadline packet"),
		destination: M.ParseSocksaddr("198.51.100.1:53"),
	}
	conn := NewSingPacketConn(stub)
	if err := conn.SetReadDeadline(time.Now().Add(time.Hour)); err != nil {
		b.Fatal(err)
	}
	buffer := buf.NewSize(64)
	defer buffer.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Reset()
		destination, err := conn.ReadPacket(buffer)
		if err != nil {
			b.Fatal(err)
		}
		if destination.Port != 53 || buffer.Len() == 0 {
			b.Fatalf("dest=%v len=%d", destination, buffer.Len())
		}
	}
}
