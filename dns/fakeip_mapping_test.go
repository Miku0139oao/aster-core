package dns

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"

	"github.com/Miku0139oao/aster-core/component/fakeip"
	C "github.com/Miku0139oao/aster-core/constant"
	icontext "github.com/Miku0139oao/aster-core/context"

	D "github.com/miekg/dns"
)

// fakeIPMappingResolver answers every A query with one fixed real IP, which is
// what a fake-ip-filtered domain receives from the upstream resolver.
type fakeIPMappingResolver struct {
	ip net.IP
}

func (r *fakeIPMappingResolver) LookupIP(context.Context, string) ([]netip.Addr, error) {
	addr, _ := netip.AddrFromSlice(r.ip)
	return []netip.Addr{addr}, nil
}

func (r *fakeIPMappingResolver) LookupIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	return r.LookupIP(ctx, host)
}

func (*fakeIPMappingResolver) LookupIPv6(context.Context, string) ([]netip.Addr, error) {
	return nil, nil
}

func (*fakeIPMappingResolver) ResolveECH(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (r *fakeIPMappingResolver) ExchangeContext(_ context.Context, m *D.Msg) (*D.Msg, error) {
	msg := new(D.Msg)
	msg.SetReply(m)
	q := m.Question[0]
	if q.Qtype == D.TypeA {
		msg.Answer = []D.RR{&D.A{
			Hdr: D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   r.ip,
		}}
	}
	return msg, nil
}

func (*fakeIPMappingResolver) Invalid() bool    { return false }
func (*fakeIPMappingResolver) ClearCache()      {}
func (*fakeIPMappingResolver) ResetConnection() {}

type fakeIPMappingMatcher string

func (m fakeIPMappingMatcher) MatchDomain(domain string) bool { return string(m) == domain }

func newFakeIPMappingHandler(t *testing.T, disableMapping bool) (handler, *ResolverEnhancer) {
	t.Helper()

	pool, err := fakeip.New(fakeip.Options{
		IPNet: netip.MustParsePrefix("198.18.0.0/16"),
		Size:  1000,
	})
	if err != nil {
		t.Fatal(err)
	}

	mapper := NewEnhancer(EnhancerConfig{
		EnhancedMode: C.DNSFakeIP,
		FakeIPPool:   pool,
		FakeIPSkipper: &fakeip.Skipper{
			// real.example is filtered out of fake-ip, so its query reaches the
			// upstream resolver and returns a real IP.
			Host: []C.DomainMatcher{fakeIPMappingMatcher("real.example")},
		},
		FakeIPTTL:            1,
		FakeIPDisableMapping: disableMapping,
	})

	resolver := &fakeIPMappingResolver{ip: net.ParseIP("8.8.8.8").To4()}
	return newHandler(resolver, mapper), mapper
}

func queryTestA(t *testing.T, h handler, host string) *D.Msg {
	t.Helper()

	request := new(D.Msg)
	request.SetQuestion(D.Fqdn(host), D.TypeA)
	response, err := h(icontext.NewDNSContext(context.Background()), request)
	if err != nil {
		t.Fatalf("query %s: %v", host, err)
	}
	return response
}

func TestFakeIPDisableMapping(t *testing.T) {
	realIP := netip.MustParseAddr("8.8.8.8")

	for _, disableMapping := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable-mapping=%t", disableMapping), func(t *testing.T) {
			h, mapper := newFakeIPMappingHandler(t, disableMapping)

			// A fake-ip-filtered domain keeps returning the real IP from the
			// upstream resolver in both modes.
			response := queryTestA(t, h, "real.example")
			if len(response.Answer) != 1 {
				t.Fatalf("real.example answers = %v, want 1", response.Answer)
			}
			a, ok := response.Answer[0].(*D.A)
			if !ok || !net.IP(a.A).Equal(net.ParseIP("8.8.8.8")) {
				t.Fatalf("real.example answer = %v, want 8.8.8.8", response.Answer[0])
			}

			host, mapped := mapper.FindHostByIP(realIP)
			if disableMapping {
				if mapped {
					t.Fatalf("mapping middleware installed with fake-ip-disable-mapping: %s -> %s", realIP, host)
				}
			} else if !mapped || host != "real.example" {
				t.Fatalf("mapping = %q, %v; want real.example -> %s", host, mapped, realIP)
			}

			// The fake-ip path itself is unchanged: a non-filtered domain still
			// receives an address from the pool.
			fakeResponse := queryTestA(t, h, "fake.example")
			if len(fakeResponse.Answer) != 1 {
				t.Fatalf("fake.example answers = %v, want 1", fakeResponse.Answer)
			}
			fakeA, ok := fakeResponse.Answer[0].(*D.A)
			if !ok {
				t.Fatalf("fake.example answer type = %T, want *dns.A", fakeResponse.Answer[0])
			}
			fakeIP, _ := netip.AddrFromSlice(fakeA.A)
			if !mapper.IsFakeIP(fakeIP) {
				t.Fatalf("fake.example answer %s is not a fake IP", fakeIP)
			}
		})
	}
}
