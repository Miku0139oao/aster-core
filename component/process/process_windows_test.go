package process

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestSearcherTCP4Match(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	const port uint16 = 443
	buf := makeTCP4Table([]tcp4Row{
		{state: 2, ip: netip.MustParseAddr("10.0.0.1"), port: port, pid: 11},
		{state: 5, ip: ip, port: port, pid: 42},
	})
	s := newSearcher(true, true)
	got, err := s.Search(buf, ip, port)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != 42 {
		t.Fatalf("pid = %d, want 42", got)
	}
}

func TestSearcherTCP4SkipsNonEstablished(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	buf := makeTCP4Table([]tcp4Row{
		{state: 2, ip: ip, port: 80, pid: 7},
	})
	s := newSearcher(true, true)
	if _, err := s.Search(buf, ip, 80); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSearcherUDP4Unspecified(t *testing.T) {
	query := netip.MustParseAddr("192.0.2.10")
	buf := makeUDP4Table([]udp4Row{
		{ip: netip.MustParseAddr("0.0.0.0"), port: 53, pid: 99},
	})
	s := newSearcher(true, false)
	got, err := s.Search(buf, query, 53)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != 99 {
		t.Fatalf("pid = %d, want 99", got)
	}
}

func TestSearcherTCP6Match(t *testing.T) {
	ip := netip.MustParseAddr("2001:db8::1")
	buf := makeTCP6Table([]tcp6Row{
		{state: 5, ip: ip, port: 8443, pid: 1234},
	})
	s := newSearcher(false, true)
	got, err := s.Search(buf, ip, 8443)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != 1234 {
		t.Fatalf("pid = %d, want 1234", got)
	}
}

func TestSearcherSameTupleDifferentPID(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	const port uint16 = 55555
	s := newSearcher(true, true)
	first, err := s.Search(makeTCP4Table([]tcp4Row{
		{state: 5, ip: ip, port: port, pid: 100},
	}), ip, port)
	if err != nil || first != 100 {
		t.Fatalf("first pid=%d err=%v", first, err)
	}
	second, err := s.Search(makeTCP4Table([]tcp4Row{
		{state: 5, ip: ip, port: port, pid: 200},
	}), ip, port)
	if err != nil || second != 200 {
		t.Fatalf("reused socket pid=%d err=%v, want 200", second, err)
	}
}

func TestShouldKeepTransportScratch(t *testing.T) {
	cases := []struct {
		ncap, used int
		keep       bool
	}{
		{4096, 100, true},
		{4096, 2048, true},
		{4096, 0, true},
		{8192, 4096, true},
		{8193, 4096, false},
		{100000, 8000, false},
		{8000, 8000, true},
		{4097, 100, false},
	}
	for _, tc := range cases {
		got := shouldKeepTransportScratch(tc.ncap, tc.used)
		if got != tc.keep {
			t.Fatalf("cap=%d used=%d keep=%v want %v", tc.ncap, tc.used, got, tc.keep)
		}
	}
}

func TestFillTransportTableOverwritesScratch(t *testing.T) {
	if err := initWin32API(); err != nil {
		t.Fatal(err)
	}
	s := &tableScratch{buf: bytes.Repeat([]byte{0xFF}, 8192)}
	used, err := fillTransportTable(s, getExTCPTable, windows.AF_INET, tcpTablePidConn)
	if err != nil {
		t.Fatal(err)
	}
	if used < 4 || len(s.buf) < 4 {
		t.Fatalf("short table used=%d len=%d", used, len(s.buf))
	}
	if readNativeUint32(s.buf) == 0xFFFFFFFF {
		t.Fatal("stale table header after fill")
	}
}

func TestFindProcessNameConcurrentLocal(t *testing.T) {
	ip, port := localTCPEndpoint(t)
	resetCachesForTest()
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, name, err := FindProcessName(TCP, ip, int(port))
			if err != nil {
				errs <- err
				return
			}
			if name == "" {
				errs <- errors.New("empty process name")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func BenchmarkSearchTCP4_4096(b *testing.B) {
	target := netip.MustParseAddr("10.0.0.200")
	const port uint16 = 443
	rows := make([]tcp4Row, 4096)
	for i := range rows {
		rows[i] = tcp4Row{
			state: 5,
			ip:    netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}),
			port:  uint16(1024 + i%50000),
			pid:   uint32(1000 + i),
		}
	}
	rows[4095] = tcp4Row{state: 5, ip: target, port: port, pid: 7777}
	buf := makeTCP4Table(rows)
	s := newSearcher(true, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid, err := s.Search(buf, target, port)
		if err != nil || pid != 7777 {
			b.Fatalf("pid=%d err=%v", pid, err)
		}
	}
}

func BenchmarkGetTransportTableTCP4(b *testing.B) {
	if err := initWin32API(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := getTransportTable(getExTCPTable, windows.AF_INET, tcpTablePidConn)
		if err != nil {
			b.Fatal(err)
		}
		if len(buf) < 4 {
			b.Fatal("short table")
		}
	}
}

func BenchmarkGetExecPathFromPID(b *testing.B) {
	pid := uint32(syscall.Getpid())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := getExecPathFromPID(pid); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFindProcessNameLocal(t *testing.T) {
	ip, port := localTCPEndpoint(t)
	resetCachesForTest()
	uid, name, err := FindProcessName(TCP, ip, int(port))
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Fatal("empty process name")
	}
	uid2, name2, err := FindProcessName(TCP, ip, int(port))
	if err != nil {
		t.Fatal(err)
	}
	if uid != uid2 || name != name2 {
		t.Fatalf("live lookup mismatch %d/%s vs %d/%s", uid, name, uid2, name2)
	}
}

func BenchmarkFindProcessNameLocal(b *testing.B) {
	ip, port := localTCPEndpoint(b)
	if _, _, err := FindProcessName(TCP, ip, int(port)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := FindProcessName(TCP, ip, int(port)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindProcessNameUncached(b *testing.B) {
	ip, port := localTCPEndpoint(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetCachesForTest()
		if _, _, err := findProcessName(TCP, ip, int(port)); err != nil {
			b.Fatal(err)
		}
	}
}

func localTCPEndpoint(tb testing.TB) (netip.Addr, uint16) {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		select {}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { conn.Close() })
	la := conn.LocalAddr().(*net.TCPAddr)
	ip, ok := netip.AddrFromSlice(la.IP)
	if !ok {
		tb.Fatalf("bad local ip %v", la.IP)
	}
	return ip.Unmap(), uint16(la.Port)
}

type tcp4Row struct {
	state uint32
	ip    netip.Addr
	port  uint16
	pid   uint32
}

type udp4Row struct {
	ip   netip.Addr
	port uint16
	pid  uint32
}

type tcp6Row struct {
	state uint32
	ip    netip.Addr
	port  uint16
	pid   uint32
}

func makeTCP4Table(rows []tcp4Row) []byte {
	buf := make([]byte, 4+24*len(rows))
	putNativeUint32(buf[:4], uint32(len(rows)))
	for i, r := range rows {
		row := buf[4+24*i : 4+24*(i+1)]
		putNativeUint32(row[0:4], r.state)
		a := r.ip.As4()
		copy(row[4:8], a[:])
		putNativeUint32(row[8:12], nativePort(r.port))
		putNativeUint32(row[20:24], r.pid)
	}
	return buf
}

func makeUDP4Table(rows []udp4Row) []byte {
	buf := make([]byte, 4+12*len(rows))
	putNativeUint32(buf[:4], uint32(len(rows)))
	for i, r := range rows {
		row := buf[4+12*i : 4+12*(i+1)]
		a := r.ip.As4()
		copy(row[0:4], a[:])
		putNativeUint32(row[4:8], nativePort(r.port))
		putNativeUint32(row[8:12], r.pid)
	}
	return buf
}

func makeTCP6Table(rows []tcp6Row) []byte {
	buf := make([]byte, 4+56*len(rows))
	putNativeUint32(buf[:4], uint32(len(rows)))
	for i, r := range rows {
		row := buf[4+56*i : 4+56*(i+1)]
		a := r.ip.As16()
		copy(row[0:16], a[:])
		putNativeUint32(row[20:24], nativePort(r.port))
		putNativeUint32(row[48:52], r.state)
		putNativeUint32(row[52:56], r.pid)
	}
	return buf
}

func nativePort(port uint16) uint32 {
	var b [4]byte
	binary.BigEndian.PutUint16(b[:2], port)
	return *(*uint32)(unsafe.Pointer(&b[0]))
}

func putNativeUint32(b []byte, v uint32) {
	*(*uint32)(unsafe.Pointer(&b[0])) = v
}
