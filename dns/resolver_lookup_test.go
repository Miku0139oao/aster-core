package dns

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/resolver"

	D "github.com/miekg/dns"
)

type staticDNSClient struct {
	aDelay, aaaaDelay time.Duration
	a, aaaa           []net.IP
}

func (c *staticDNSClient) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	q := m.Question[0]
	var delay time.Duration
	var ips []net.IP
	switch q.Qtype {
	case D.TypeA:
		delay, ips = c.aDelay, c.a
	case D.TypeAAAA:
		delay, ips = c.aaaaDelay, c.aaaa
	default:
		msg := new(D.Msg)
		msg.SetRcode(m, D.RcodeNameError)
		return msg, nil
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	msg := new(D.Msg)
	msg.SetReply(m)
	for _, ip := range ips {
		switch q.Qtype {
		case D.TypeA:
			msg.Answer = append(msg.Answer, &D.A{
				Hdr: D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
				A:   ip,
			})
		case D.TypeAAAA:
			msg.Answer = append(msg.Answer, &D.AAAA{
				Hdr:  D.RR_Header{Name: q.Name, Rrtype: D.TypeAAAA, Class: D.ClassINET, Ttl: 60},
				AAAA: ip,
			})
		}
	}
	return msg, nil
}

func (c *staticDNSClient) Address() string { return "static" }

func (c *staticDNSClient) ResetConnection() {}

func testResolver(client dnsClient, ipv6Timeout time.Duration) *Resolver {
	return &Resolver{
		ipv6:        true,
		ipv6Timeout: ipv6Timeout,
		main:        []dnsClient{client},
		cache:       Config{}.newCache(),
	}
}

func TestLookupIPWaitsForAAAAWhenAFails(t *testing.T) {
	client := &staticDNSClient{
		aaaaDelay: 200 * time.Millisecond,
		aaaa:      []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)

	start := time.Now()
	ips, err := r.LookupIP(context.Background(), "ipv6-only.example")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LookupIP returned error: %v", err)
	}
	if len(ips) != 1 || ips[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("LookupIP ips = %v, want 2001:db8::1", ips)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("LookupIP returned too quickly (%v); AAAA should not be cut off by ipv6Timeout when A fails", elapsed)
	}
}

func TestLookupIPKeepsIPv6TimeoutAfterASucceeds(t *testing.T) {
	client := &staticDNSClient{
		a:         []net.IP{net.ParseIP("192.0.2.1").To4()},
		aaaaDelay: 200 * time.Millisecond,
		aaaa:      []net.IP{net.ParseIP("2001:db8::1")},
	}
	r := testResolver(client, 50*time.Millisecond)

	start := time.Now()
	ips, err := r.LookupIP(context.Background(), "dual.example")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LookupIP returned error: %v", err)
	}
	if len(ips) != 1 || ips[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("LookupIP ips = %v, want only IPv4 when AAAA exceeds ipv6Timeout", ips)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("LookupIP waited %v; ipv6Timeout should still apply after A succeeds", elapsed)
	}
}

func TestLookupIPBothFail(t *testing.T) {
	r := testResolver(&staticDNSClient{}, 50*time.Millisecond)
	ips, err := r.LookupIP(context.Background(), "missing.example")
	if err != resolver.ErrIPNotFound {
		t.Fatalf("LookupIP err = %v, want ErrIPNotFound", err)
	}
	if len(ips) != 0 {
		t.Fatalf("LookupIP ips = %v, want empty", ips)
	}
}
