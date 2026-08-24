package dns

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	icontext "github.com/Miku0139oao/aster-core/context"

	D "github.com/miekg/dns"
)

func dnsQuestion(name string, qtype uint16) D.Question {
	return D.Question{Name: D.Fqdn(name), Qtype: qtype, Qclass: D.ClassINET}
}

func collectForTest(name string, qtype uint16, msg *D.Msg) []kerneldirect.DNSAnswer {
	msg.Response = true
	msg.Rcode = D.RcodeSuccess
	return collectKernelDirectAnswers(dnsQuestion(name, qtype), msg)
}

func kernelDirectAnswerSet(answers []kerneldirect.DNSAnswer) map[netip.Addr]time.Duration {
	set := make(map[netip.Addr]time.Duration, len(answers))
	for _, answer := range answers {
		set[answer.Addr] = answer.TTL
	}
	return set
}

func TestKernelDirectObservesRealDNSAnswers(t *testing.T) {
	var current kerneldirect.DecisionSets
	closer := kerneldirect.Register(func(host string, _ netip.Addr) bool {
		return host == "direct.example"
	}, func(sets kerneldirect.DecisionSets) {
		current = sets
	})
	defer closer.Close()

	endpoint := func(_ *icontext.DNSContext, request *D.Msg) (*D.Msg, error) {
		response := new(D.Msg)
		response.SetReply(request)
		response.Answer = []D.RR{&D.A{
			Hdr: D.RR_Header{Name: request.Question[0].Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   net.ParseIP("8.8.8.8").To4(),
		}}
		return response, nil
	}
	h := withKernelDirectObservation()(endpoint)
	request := new(D.Msg)
	request.SetQuestion("direct.example.", D.TypeA)
	if _, err := h(icontext.NewDNSContext(context.Background()), request); err != nil {
		t.Fatal(err)
	}

	addr := netip.MustParseAddr("8.8.8.8")
	if current.Direct == nil || !current.Direct.Contains(addr) {
		t.Fatal("real DNS answer did not reach kernel-direct controller")
	}
}

func TestKernelDirectObservesTerminalExtraAddress(t *testing.T) {
	var current kerneldirect.DecisionSets
	closer := kerneldirect.Register(func(host string, _ netip.Addr) bool {
		return host == "iwx.smoba.qq.com"
	}, func(sets kerneldirect.DecisionSets) {
		current = sets
	})
	defer closer.Close()

	endpoint := func(_ *icontext.DNSContext, request *D.Msg) (*D.Msg, error) {
		response := new(D.Msg)
		response.SetReply(request)
		response.Answer = []D.RR{&D.CNAME{
			Hdr:    D.RR_Header{Name: request.Question[0].Name, Rrtype: D.TypeCNAME, Class: D.ClassINET, Ttl: 10},
			Target: "edge.example.",
		}}
		response.Extra = []D.RR{&D.A{
			Hdr: D.RR_Header{Name: "edge.example.", Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
			A:   net.ParseIP("129.226.1.1").To4(),
		}}
		return response, nil
	}
	h := withKernelDirectObservation()(endpoint)
	request := new(D.Msg)
	request.SetQuestion("iwx.smoba.qq.com.", D.TypeA)
	if _, err := h(icontext.NewDNSContext(context.Background()), request); err != nil {
		t.Fatal(err)
	}

	addr := netip.MustParseAddr("129.226.1.1")
	if current.Direct == nil || !current.Direct.Contains(addr) {
		t.Fatal("terminal CNAME address in Extra did not reach kernel-direct")
	}
}

func TestKernelDirectAliasChainIsTerminalAndTTLBounded(t *testing.T) {
	msg := &D.Msg{
		Answer: []D.RR{
			&D.CNAME{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 10}, Target: "hop.one."},
			&D.CNAME{Hdr: D.RR_Header{Name: "hop.one.", Class: D.ClassINET, Ttl: 2}, Target: "hop.two."},
			&D.A{Hdr: D.RR_Header{Name: "unrelated.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("1.1.1.1").To4()},
			&D.A{Hdr: D.RR_Header{Name: "hop.one.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("4.4.4.4").To4()},
		},
		Extra: []D.RR{
			&D.CNAME{Hdr: D.RR_Header{Name: "hop.two.", Class: D.ClassINET, Ttl: 1}, Target: "poison.example."},
			&D.A{Hdr: D.RR_Header{Name: "hop.two.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("8.8.4.4").To4()},
			&D.A{Hdr: D.RR_Header{Name: "poison.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("9.9.9.9").To4()},
		},
	}

	got := kernelDirectAnswerSet(collectForTest("query.example", D.TypeA, msg))
	if ttl, ok := got[netip.MustParseAddr("8.8.4.4")]; !ok || ttl != 2*time.Second {
		t.Fatalf("terminal address TTL = %v, present=%v, want 2s", ttl, ok)
	}
	for _, rejected := range []string{"1.1.1.1", "4.4.4.4", "9.9.9.9"} {
		if _, ok := got[netip.MustParseAddr(rejected)]; ok {
			t.Fatalf("non-terminal or Extra-authorized address %s was collected", rejected)
		}
	}
}

func TestKernelDirectDNAMETraversal(t *testing.T) {
	t.Run("synthesized owner and TTL", func(t *testing.T) {
		msg := &D.Msg{
			Answer: []D.RR{&D.DNAME{Hdr: D.RR_Header{Name: "old.example.", Class: D.ClassINET, Ttl: 5}, Target: "new.example."}},
			Extra: []D.RR{
				&D.A{Hdr: D.RR_Header{Name: "www.branch.new.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("9.9.9.9").To4()},
				&D.A{Hdr: D.RR_Header{Name: "new.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("1.0.0.1").To4()},
			},
		}
		got := kernelDirectAnswerSet(collectForTest("www.branch.old.example", D.TypeA, msg))
		if ttl := got[netip.MustParseAddr("9.9.9.9")]; ttl != 5*time.Second {
			t.Fatalf("DNAME address TTL = %v, want 5s", ttl)
		}
		if _, ok := got[netip.MustParseAddr("1.0.0.1")]; ok {
			t.Fatal("DNAME target apex replaced a synthesized owner")
		}
	})

	t.Run("root target preserves unmatched prefix", func(t *testing.T) {
		msg := &D.Msg{
			Answer: []D.RR{&D.DNAME{Hdr: D.RR_Header{Name: "old.example.", Class: D.ClassINET, Ttl: 10}, Target: "."}},
			Extra:  []D.RR{&D.A{Hdr: D.RR_Header{Name: "www.branch.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("4.4.4.4").To4()}},
		}
		got := kernelDirectAnswerSet(collectForTest("www.branch.old.example", D.TypeA, msg))
		if _, ok := got[netip.MustParseAddr("4.4.4.4")]; !ok {
			t.Fatal("root-target DNAME did not preserve unmatched labels")
		}
	})

	t.Run("label boundary", func(t *testing.T) {
		msg := &D.Msg{
			Answer: []D.RR{&D.DNAME{Hdr: D.RR_Header{Name: "old.example.", Class: D.ClassINET, Ttl: 10}, Target: "new.example."}},
			Extra:  []D.RR{&D.A{Hdr: D.RR_Header{Name: "notnew.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("4.4.4.4").To4()}},
		}
		if got := collectForTest("notold.example", D.TypeA, msg); len(got) != 0 {
			t.Fatal("DNAME substitution crossed a DNS label boundary")
		}
	})
}

func TestKernelDirectRejectsAmbiguousAndCyclicAliases(t *testing.T) {
	for name, msg := range map[string]*D.Msg{
		"ambiguous": {
			Answer: []D.RR{
				&D.CNAME{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 10}, Target: "one.example."},
				&D.CNAME{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 10}, Target: "two.example."},
			},
		},
		"cycle": {
			Answer: []D.RR{
				&D.CNAME{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 10}, Target: "one.example."},
				&D.CNAME{Hdr: D.RR_Header{Name: "one.example.", Class: D.ClassINET, Ttl: 10}, Target: "query.example."},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := collectForTest("query.example", D.TypeA, msg); len(got) != 0 {
				t.Fatalf("invalid alias graph produced %d answers", len(got))
			}
		})
	}
}

func TestKernelDirectDeduplicatesWithMinimumTTLAndQuestionFamily(t *testing.T) {
	msg := &D.Msg{Answer: []D.RR{
		&D.A{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 300}, A: net.ParseIP("8.8.8.8").To4()},
		&D.A{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 30}, A: net.ParseIP("8.8.8.8").To4()},
		&D.AAAA{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 1}, AAAA: net.ParseIP("2001:4860:4860::8888").To16()},
	}}
	answers := collectForTest("query.example", D.TypeA, msg)
	if len(answers) != 1 || answers[0].Addr != netip.MustParseAddr("8.8.8.8") || answers[0].TTL != 30*time.Second {
		t.Fatalf("unexpected deduplicated answers: %#v", answers)
	}
}

func TestKernelDirectResponseEligibilityAndTypedNilSafety(t *testing.T) {
	base := &D.Msg{Answer: []D.RR{
		(*D.A)(nil),
		(*D.AAAA)(nil),
		(*D.CNAME)(nil),
		(*D.DNAME)(nil),
		&D.A{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 20}, A: net.ParseIP("8.8.8.8").To4()},
	}}
	if got := collectForTest("query.example", D.TypeA, base); len(got) != 1 {
		t.Fatalf("typed nil records changed valid collection: %#v", got)
	}

	eligible := &D.Msg{Answer: []D.RR{&D.A{Hdr: D.RR_Header{Name: "query.example.", Class: D.ClassINET, Ttl: 20}, A: net.ParseIP("8.8.8.8").To4()}}}
	for name, mutate := range map[string]func(*D.Msg){
		"not response": func(msg *D.Msg) { msg.Response = false },
		"truncated":    func(msg *D.Msg) { msg.Truncated = true },
		"nxdomain":     func(msg *D.Msg) { msg.Rcode = D.RcodeNameError },
	} {
		t.Run(name, func(t *testing.T) {
			msg := eligible.Copy()
			msg.Response = true
			msg.Rcode = D.RcodeSuccess
			mutate(msg)
			if got := collectKernelDirectAnswers(dnsQuestion("query.example", D.TypeA), msg); len(got) != 0 {
				t.Fatalf("ineligible response produced %d answers", len(got))
			}
		})
	}
	if got := collectKernelDirectAnswers(D.Question{Name: "query.example.", Qtype: D.TypeTXT, Qclass: D.ClassINET}, func() *D.Msg {
		msg := eligible.Copy()
		msg.Response = true
		return msg
	}()); len(got) != 0 {
		t.Fatal("non-address question produced kernel-direct answers")
	}
}

func TestKernelDirectObservationNilSafe(t *testing.T) {
	h := withKernelDirectObservation()(func(_ *icontext.DNSContext, _ *D.Msg) (*D.Msg, error) {
		return nil, nil
	})
	if _, err := h(icontext.NewDNSContext(context.Background()), nil); err != nil {
		t.Fatal(err)
	}

	empty := new(D.Msg)
	if _, err := h(icontext.NewDNSContext(context.Background()), empty); err != nil {
		t.Fatal(err)
	}
}
