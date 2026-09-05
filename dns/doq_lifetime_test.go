package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/ca"

	"github.com/metacubex/quic-go"
	"github.com/metacubex/tls"
	D "github.com/miekg/dns"
)

func startDoQLoopback(t *testing.T) (addr string, accepts *atomic.Int32) {
	t.Helper()
	certPEM, keyPEM, _, err := ca.NewRandomTLSKeyPair(ca.KeyPairTypeP256)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{NextProtoDQ},
	}
	ln, err := quic.ListenAddr("127.0.0.1:0", tlsConf, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	accepts = new(atomic.Int32)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept(ctx)
			if err != nil {
				return
			}
			accepts.Add(1)
			wg.Add(1)
			go func(c *quic.Conn) {
				defer wg.Done()
				serveDoQConn(ctx, c)
			}(conn)
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		wg.Wait()
	})
	return ln.Addr().String(), accepts
}

func serveDoQConn(ctx context.Context, conn *quic.Conn) {
	defer conn.CloseWithError(QUICCodeNoError, "")
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go serveDoQStream(stream)
	}
}

func serveDoQStream(stream *quic.Stream) {
	defer stream.Close()
	var hdr [2]byte
	if _, err := io.ReadFull(stream, hdr[:]); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n == 0 || n > MaxMsgSize {
		return
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(stream, buf); err != nil {
		return
	}
	req := new(D.Msg)
	if err := req.Unpack(buf); err != nil {
		return
	}
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
		return
	}
	out := make([]byte, 2+len(packed))
	binary.BigEndian.PutUint16(out, uint16(len(packed)))
	copy(out[2:], packed)
	_, _ = stream.Write(out)
}

func newTestDoQ(t *testing.T, addr string) *dnsOverQUIC {
	t.Helper()
	c := newDoQ(addr, nil, map[string]string{"skip-cert-verify": "true"}, nil, "")
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func waitDoQAccepts(t *testing.T, accepts *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for accepts.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("server accepts=%d, want %d", accepts.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDoQConcurrentColdStartSingleFlight(t *testing.T) {
	addr, accepts := startDoQLoopback(t)
	doq := newTestDoQ(t, addr)

	const n = 16
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			msg, err := doq.ExchangeContext(ctx, newAQuestion())
			if err != nil {
				errCh <- err
				return
			}
			if msg == nil || len(msg.Answer) != 1 {
				errCh <- errors.New("missing answer")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if got := accepts.Load(); got != 1 {
		t.Fatalf("server accepts=%d, want 1 shared QUIC conn", got)
	}
	if !doq.hasConnection() {
		t.Fatal("expected cached connection after cold start")
	}
}

func TestDoQConcurrentReconnectReusesWinner(t *testing.T) {
	addr, accepts := startDoQLoopback(t)
	doq := newTestDoQ(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := doq.ExchangeContext(ctx, newAQuestion()); err != nil {
		t.Fatal(err)
	}
	if got := accepts.Load(); got != 1 {
		t.Fatalf("after first exchange accepts=%d, want 1", got)
	}
	stale, err := doq.getConnection(ctx, true)
	if err != nil || stale == nil {
		t.Fatalf("cached conn: %v", err)
	}

	const n = 8
	var ready, begin, wg sync.WaitGroup
	errCh := make(chan error, n)
	conns := make([]*quic.Conn, n)
	ready.Add(n)
	begin.Add(1)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			ready.Done()
			begin.Wait()
			conn, err := doq.getConnectionObserved(ctx, false, stale)
			if err != nil {
				errCh <- err
				return
			}
			conns[i] = conn
		}()
	}
	ready.Wait()
	begin.Done()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	winner := conns[0]
	if winner == nil {
		t.Fatal("missing winner conn")
	}
	if winner == stale {
		t.Fatal("reconnect returned the stale conn instead of dialing a replacement")
	}
	for i, c := range conns {
		if c != winner {
			t.Fatalf("goroutine %d got a different conn; concurrent useCached=false should reuse the published winner", i)
		}
	}
	waitDoQAccepts(t, accepts, 2)
}

func TestDoQCloseThenExchangeErrClosed(t *testing.T) {
	addr, _ := startDoQLoopback(t)
	doq := newTestDoQ(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := doq.ExchangeContext(ctx, newAQuestion()); err != nil {
		t.Fatal(err)
	}
	if err := doq.Close(); err != nil {
		t.Fatal(err)
	}
	if doq.hasConnection() {
		t.Fatal("Close should nil the cached conn")
	}
	_, err := doq.ExchangeContext(ctx, newAQuestion())
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Exchange after Close err=%v, want net.ErrClosed", err)
	}
}

func TestDoQResetStaysReusable(t *testing.T) {
	addr, accepts := startDoQLoopback(t)
	doq := newTestDoQ(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := doq.ExchangeContext(ctx, newAQuestion()); err != nil {
		t.Fatal(err)
	}
	doq.ResetConnection()
	if doq.hasConnection() {
		t.Fatal("ResetConnection should drop the cached conn")
	}
	if _, err := doq.ExchangeContext(ctx, newAQuestion()); err != nil {
		t.Fatal(err)
	}
	if got := accepts.Load(); got != 2 {
		t.Fatalf("after Reset+Exchange accepts=%d, want 2", got)
	}
}
