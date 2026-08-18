package resolver

import (
	"net/netip"
	"testing"
	"time"

	"github.com/Miku0139oao/aster-core/component/trie"
)

func TestHostsSearchFollowsAliasChain(t *testing.T) {
	tree := trie.New[HostValue]()
	alias, err := NewHostValueByDomain("bar.example")
	if err != nil {
		t.Fatal(err)
	}
	ip, err := NewHostValueByIPs([]netip.Addr{netip.MustParseAddr("192.0.2.1")})
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert("foo.example", alias); err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert("bar.example", ip); err != nil {
		t.Fatal(err)
	}

	hosts := NewHosts(tree)
	got, ok := hosts.Search("foo.example", false)
	if !ok {
		t.Fatal("expected alias chain to resolve")
	}
	if got.IsDomain || len(got.IPs) != 1 || got.IPs[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("got %+v, want 192.0.2.1", got)
	}
}

func TestHostsSearchCycleDoesNotHang(t *testing.T) {
	tree := trie.New[HostValue]()
	a, err := NewHostValueByDomain("b.example")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewHostValueByDomain("a.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert("a.example", a); err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert("b.example", b); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		hosts := NewHosts(tree)
		_, _ = hosts.Search("a.example", false)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Hosts.Search hung on a cyclic alias")
	}
}

func TestHostsSearchSelfAliasDoesNotHang(t *testing.T) {
	tree := trie.New[HostValue]()
	self, err := NewHostValueByDomain("loop.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert("loop.example", self); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		hosts := NewHosts(tree)
		_, _ = hosts.Search("loop.example", false)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Hosts.Search hung on a self-alias")
	}
}
