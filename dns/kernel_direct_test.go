package dns

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	icontext "github.com/Miku0139oao/aster-core/context"

	D "github.com/miekg/dns"
)

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
