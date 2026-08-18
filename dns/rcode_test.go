package dns

import (
	"context"
	"testing"

	D "github.com/miekg/dns"
)

func TestRCodeClientDoesNotMutateRequest(t *testing.T) {
	client := newRCodeClient("refused")
	req := new(D.Msg)
	req.SetQuestion("example.com.", D.TypeA)
	req.RecursionDesired = true

	resp, err := client.ExchangeContext(context.Background(), req)
	if err != nil {
		t.Fatalf("ExchangeContext: %v", err)
	}
	if req.Response {
		t.Fatal("request was mutated to a response")
	}
	if req.Rcode != D.RcodeSuccess {
		t.Fatalf("request Rcode mutated to %d", req.Rcode)
	}
	if resp == req {
		t.Fatal("rcode client returned the request pointer")
	}
	if !resp.Response {
		t.Fatal("response is not marked as a response")
	}
	if resp.Rcode != D.RcodeRefused {
		t.Fatalf("response Rcode = %d, want refused", resp.Rcode)
	}
	if len(resp.Question) != 1 || resp.Question[0] != req.Question[0] {
		t.Fatalf("response question = %v, want copy of request", resp.Question)
	}
}
