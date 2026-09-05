package dns

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/common/pool"

	D "github.com/miekg/dns"
)

type stubDNSClient struct {
	addr string
}

func (c stubDNSClient) ExchangeContext(context.Context, *D.Msg) (*D.Msg, error) {
	return nil, nil
}

func (c stubDNSClient) Address() string { return c.addr }

func (c stubDNSClient) ResetConnection() {}

func newAQuestion() *D.Msg {
	m := new(D.Msg)
	m.SetQuestion("www.example.com.", D.TypeA)
	m.Id = 0x1234
	m.RecursionDesired = true
	return m
}

func seededSystemClient() *systemClient {
	c := newSystemClient()
	c.lastFlush = time.Now()
	for _, addr := range []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"} {
		c.dnsClients[addr] = &systemDnsClient{
			dnsClient: stubDNSClient{addr: addr + ":53"},
		}
	}
	return c
}

func TestPackDNSQueryID0DoesNotMutateMsg(t *testing.T) {
	m := newAQuestion()
	buf := make([]byte, 512)
	packed, origID, err := packDNSQueryID0(m, buf)
	if err != nil {
		t.Fatal(err)
	}
	if origID != 0x1234 {
		t.Fatalf("origID=%d", origID)
	}
	if m.Id != 0x1234 {
		t.Fatalf("caller msg id mutated: %d", m.Id)
	}
	if got := binary.BigEndian.Uint16(packed[:2]); got != 0 {
		t.Fatalf("wire id=%d, want 0", got)
	}
}

func TestBuildDoHGETURL(t *testing.T) {
	m := newAQuestion()
	packed, _, err := packDNSQueryID0(m, make([]byte, 512))
	if err != nil {
		t.Fatal(err)
	}
	doh := &dnsOverHTTPS{urlPrefix: "https://dns.google/dns-query?dns="}
	got := doh.buildDoHGETURL(packed)
	want := "https://dns.google/dns-query?dns=" + base64.RawURLEncoding.EncodeToString(packed)
	if got != want {
		t.Fatalf("url=%q want %q", got, want)
	}
}

type stallReader struct{}

func (stallReader) Read([]byte) (int, error) { return 0, nil }

func TestReadDNSBody(t *testing.T) {
	payload := bytes.Repeat([]byte{1, 2, 3, 4}, 64)
	buf := make([]byte, 1024)
	got, err := readDNSBody(bytes.NewReader(payload), buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("len=%d", len(got))
	}

	small := make([]byte, len(payload)-1)
	if _, err := readDNSBody(bytes.NewReader(payload), small); err == nil {
		t.Fatal("expected too-large error")
	}

	if _, err := readDNSBody(stallReader{}, buf); err != io.ErrNoProgress {
		t.Fatalf("stalled reader: %v", err)
	}
}

func TestExchangeLengthPrefixedConn(t *testing.T) {
	req := newAQuestion()
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{
		&D.A{
			Hdr: D.RR_Header{Name: "www.example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   net.IPv4(93, 184, 216, 34),
		},
	}
	packedResp, err := resp.Pack()
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		var hdr [2]byte
		if _, err := io.ReadFull(serverConn, hdr[:]); err != nil {
			errCh <- err
			return
		}
		n := int(binary.BigEndian.Uint16(hdr[:]))
		buf := make([]byte, n)
		if _, err := io.ReadFull(serverConn, buf); err != nil {
			errCh <- err
			return
		}
		q := new(D.Msg)
		if err := q.Unpack(buf); err != nil {
			errCh <- err
			return
		}
		if q.Id != req.Id {
			errCh <- errMismatch{q.Id}
			return
		}
		binary.BigEndian.PutUint16(hdr[:], uint16(len(packedResp)))
		if _, err := serverConn.Write(hdr[:]); err != nil {
			errCh <- err
			return
		}
		_, err := serverConn.Write(packedResp)
		errCh <- err
	}()

	msg, err := exchangeLengthPrefixedConn(clientConn, req, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || len(msg.Answer) != 1 {
		t.Fatal("missing answer")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

type errMismatch struct{ id uint16 }

func (e errMismatch) Error() string { return "id mismatch" }

func TestSystemGetDnsClientsSnapshot(t *testing.T) {
	c := seededSystemClient()
	first, err := c.getDnsClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("got %d clients", len(first))
	}
	second, err := c.getDnsClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 3 {
		t.Fatalf("got %d clients", len(second))
	}
	if &first[0] != &second[0] {
		t.Fatal("hot path did not reuse live snapshot")
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clients, err := c.getDnsClients()
			if err != nil || len(clients) != 3 {
				t.Errorf("concurrent getDnsClients: n=%d err=%v", len(clients), err)
			}
		}()
	}
	wg.Wait()
}

func TestSetEdns0Subnet(t *testing.T) {
	m := newAQuestion()
	prefix := netip.MustParsePrefix("1.2.3.0/24")
	if !setEdns0Subnet(m, prefix, true) {
		t.Fatal("expected subnet to be set")
	}
	opt := m.IsEdns0()
	if opt == nil {
		t.Fatal("missing OPT")
	}
	var found *D.EDNS0_SUBNET
	for _, o := range opt.Option {
		if s, ok := o.(*D.EDNS0_SUBNET); ok {
			found = s
			break
		}
	}
	if found == nil || found.Address.String() != "1.2.3.0" {
		t.Fatalf("subnet=%v", found)
	}
}

func BenchmarkDoHQueryPrepare(b *testing.B) {
	m := newAQuestion()
	doh := &dnsOverHTTPS{urlPrefix: "https://dns.google/dns-query?dns="}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packBuf := pool.Get(512)
		packed, _, err := packDNSQueryID0(m, packBuf)
		if err != nil {
			b.Fatal(err)
		}
		_ = doh.buildDoHGETURL(packed)
		_ = pool.Put(packBuf)
	}
}

func BenchmarkDoQQueryPrepare(b *testing.B) {
	m := newAQuestion()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(2 + MaxMsgSize)
		packed, _, err := packDNSQueryID0(m, buf[2:])
		if err != nil {
			_ = pool.Put(buf)
			b.Fatal(err)
		}
		if err := putLengthPrefixed(buf, packed); err != nil {
			_ = pool.Put(buf)
			b.Fatal(err)
		}
		_ = buf[:2+len(packed)]
		_ = pool.Put(buf)
	}
}

func BenchmarkSystemGetDnsClients(b *testing.B) {
	c := seededSystemClient()
	if _, err := c.getDnsClients(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clients, err := c.getDnsClients()
		if err != nil {
			b.Fatal(err)
		}
		if len(clients) != 3 {
			b.Fatalf("got %d clients", len(clients))
		}
	}
}

func BenchmarkSetEdns0Subnet(b *testing.B) {
	prefix := netip.MustParsePrefix("1.2.3.0/24")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := newAQuestion()
		if !setEdns0Subnet(m, prefix, true) {
			b.Fatal("expected subnet to be set")
		}
	}
}

func BenchmarkDoTExchangeWithConn(b *testing.B) {
	req := newAQuestion()
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{
		&D.A{
			Hdr: D.RR_Header{Name: "www.example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   net.IPv4(93, 184, 216, 34),
		},
	}
	packedResp, err := resp.Pack()
	if err != nil {
		b.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var hdr [2]byte
		buf := make([]byte, 4096)
		for {
			if _, err := io.ReadFull(serverConn, hdr[:]); err != nil {
				return
			}
			n := int(binary.BigEndian.Uint16(hdr[:]))
			if _, err := io.ReadFull(serverConn, buf[:n]); err != nil {
				return
			}
			binary.BigEndian.PutUint16(hdr[:], uint16(len(packedResp)))
			if _, err := serverConn.Write(hdr[:]); err != nil {
				return
			}
			if _, err := serverConn.Write(packedResp); err != nil {
				return
			}
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, err := exchangeLengthPrefixedConn(clientConn, req, 5*time.Second)
		if err != nil {
			b.Fatal(err)
		}
		if msg == nil || len(msg.Answer) != 1 {
			b.Fatal("missing answer")
		}
	}
	b.StopTimer()
	clientConn.Close()
	wg.Wait()
}

func BenchmarkDoHReadAll(b *testing.B) {
	payload := bytes.Repeat([]byte{1, 2, 3, 4}, 64) // 256-byte typical DoH body
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(MaxMsgSize)
		_, err := readDNSBody(bytes.NewReader(payload), buf)
		_ = pool.Put(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func packedAReply(req *D.Msg) []byte {
	resp := new(D.Msg)
	resp.SetReply(req)
	resp.Answer = []D.RR{
		&D.A{
			Hdr: D.RR_Header{Name: "www.example.com.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   net.IPv4(93, 184, 216, 34),
		},
	}
	packed, err := resp.Pack()
	if err != nil {
		panic(err)
	}
	return packed
}

func writeLengthPrefixed(w io.Writer, payload []byte) error {
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readLengthPrefixedQuery(r io.Reader) error {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return err
}

func serveDoTReply(server net.Conn, packedResp []byte) error {
	if err := readLengthPrefixedQuery(server); err != nil {
		return err
	}
	return writeLengthPrefixed(server, packedResp)
}

// raceDeadlineConn delays SetDeadline(now) so a success-vs-cancel race is controllable.
type raceDeadlineConn struct {
	net.Conn
	mu        sync.Mutex
	last      time.Time
	deadlines []time.Time
	delayNow  time.Duration
	cancel    context.CancelFunc
	reads     int
	afterRead int
}

func (c *raceDeadlineConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err == nil && c.cancel != nil {
		c.reads++
		if c.reads == c.afterRead {
			c.cancel()
		}
	}
	return n, err
}

func (c *raceDeadlineConn) SetDeadline(t time.Time) error {
	if !t.IsZero() && !t.After(time.Now().Add(50*time.Millisecond)) && c.delayNow > 0 {
		time.Sleep(c.delayNow)
	}
	c.mu.Lock()
	c.last = t
	c.deadlines = append(c.deadlines, t)
	c.mu.Unlock()
	return c.Conn.SetDeadline(t)
}

func TestDoTCallbackVsSuccessRace(t *testing.T) {
	req := newAQuestion()
	packedResp := packedAReply(req)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapped := &raceDeadlineConn{
		Conn:      clientConn,
		delayNow:  50 * time.Millisecond,
		cancel:    cancel,
		afterRead: 2, // length prefix + body; I/O has succeeded before AfterFunc runs
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveDoTReply(serverConn, packedResp)
	}()

	msg, err := exchangeLengthPrefixedConnContext(ctx, wrapped, req, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || len(msg.Answer) != 1 {
		t.Fatal("missing answer")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	// If stop() did not join the callback, SetDeadline(now) would land after
	// SetDeadline(zero) and poison the next borrower.
	time.Sleep(80 * time.Millisecond)
	wrapped.mu.Lock()
	last := wrapped.last
	wrapped.mu.Unlock()
	if !last.IsZero() {
		t.Fatalf("last deadline %v, want zero after joining cancel callback", last)
	}
}

func TestDoTExchangeContextImmediateCancel(t *testing.T) {
	req := newAQuestion()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	dot := &dnsOverTLS{}
	dot.connections.SetBaseCap(maxOldDotConns)
	dot.connections.PushBack(clientConn)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := dot.ExchangeContext(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if dot.connections.Len() != 1 {
		t.Fatalf("immediate cancel popped idle conn: len=%d", dot.connections.Len())
	}
}

func TestDoTExchangeContextCancelClosesConn(t *testing.T) {
	req := newAQuestion()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	dot := &dnsOverTLS{}
	dot.connections.SetBaseCap(maxOldDotConns)
	dot.connections.PushBack(clientConn)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := dot.ExchangeContext(ctx, req)
		errCh <- err
	}()

	if err := readLengthPrefixedQuery(serverConn); err != nil {
		t.Fatal(err)
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ExchangeContext did not return after cancel")
	}

	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatal("expected aborted conn to close")
	}
	if dot.connections.Len() != 0 {
		t.Fatalf("aborted conn was re-pooled: len=%d", dot.connections.Len())
	}
}

func TestDoTReuseStaleRetry(t *testing.T) {
	req := newAQuestion()
	packedResp := packedAReply(req)

	goodClient, goodServer := net.Pipe()
	defer goodClient.Close()
	defer goodServer.Close()
	staleClient, staleServer := net.Pipe()
	_ = staleServer.Close()
	defer staleClient.Close()

	dot := &dnsOverTLS{}
	dot.connections.SetBaseCap(maxOldDotConns)
	// LIFO: stale is popped first, then the healthy conn on retry.
	dot.connections.PushBack(goodClient)
	dot.connections.PushBack(staleClient)

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveDoTReply(goodServer, packedResp)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := dot.ExchangeContext(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || len(msg.Answer) != 1 {
		t.Fatal("missing answer")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if dot.connections.Len() != 1 {
		t.Fatalf("want 1 pooled conn after stale retry, got %d", dot.connections.Len())
	}
}

func TestDoTCloseBeforeLateSuccess(t *testing.T) {
	req := newAQuestion()
	packedResp := packedAReply(req)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	dot := &dnsOverTLS{}
	dot.connections.SetBaseCap(maxOldDotConns)
	dot.connections.PushBack(clientConn)

	type result struct {
		msg *D.Msg
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		msg, err := dot.ExchangeContext(context.Background(), req)
		resCh <- result{msg, err}
	}()

	if err := readLengthPrefixedQuery(serverConn); err != nil {
		t.Fatal(err)
	}
	if err := dot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeLengthPrefixed(serverConn, packedResp); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("in-flight exchange: %v", res.err)
		}
		if res.msg == nil || len(res.msg.Answer) != 1 {
			t.Fatal("missing in-flight answer")
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight exchange did not finish after Close")
	}

	if dot.connections.Len() != 0 {
		t.Fatalf("late success re-pooled after Close: len=%d", dot.connections.Len())
	}
	_, err := dot.ExchangeContext(context.Background(), req)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("subsequent Exchange err=%v, want net.ErrClosed", err)
	}
}
