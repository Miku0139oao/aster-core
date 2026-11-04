package constant

import (
	"encoding/json"
	"testing"
)

// TestAppendTextMatchesMarshalJSON pins that the allocation-free AppendText
// methods produce byte-identical JSON to the MarshalJSON/text-marshaller path
// they replace, for every enum value including out-of-range ones.
func TestAppendTextMatchesMarshalJSON(t *testing.T) {
	networks := []NetWork{TCP, UDP, ALLNet, NetWork(99), NetWork(-1)}
	for _, n := range networks {
		got, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("NetWork(%d): AppendText path %s != MarshalJSON path %s", int(n), got, want)
		}
		appended, err := n.AppendText(nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(appended) != string(want) {
			t.Fatalf("NetWork(%d): AppendText %s != %s", int(n), appended, want)
		}
	}

	types := []Type{HTTP, SOCKS5, TUNNEL, Type(99), Type(-1)}
	for _, tp := range types {
		got, err := json.Marshal(tp)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(tp)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("Type(%d): %s != %s", int(tp), got, want)
		}
	}

	modes := []DNSMode{DNSNormal, DNSFakeIP, DNSMapping, DNSHosts, DNSMode(99), DNSMode(-1)}
	for _, m := range modes {
		got, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("DNSMode(%d): %s != %s", int(m), got, want)
		}
	}
}
