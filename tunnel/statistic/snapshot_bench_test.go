package statistic

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/gofrs/uuid/v5"
)

// benchTracker is a Tracker with a prebuilt Info, so the benchmark measures
// Snapshot + JSON encoding rather than tracker construction.
type benchTracker struct {
	Tracker
	info TrackerInfo
}

func (b *benchTracker) Info() *TrackerInfo { return &b.info }

func benchManager(n int) *Manager {
	m := &Manager{}
	meta := &C.Metadata{
		NetWork:  C.TCP,
		Type:     C.SOCKS5,
		SrcIP:    netip.MustParseAddr("192.0.2.10"),
		DstIP:    netip.MustParseAddr("203.0.113.20"),
		SrcPort:  51000,
		DstPort:  443,
		Host:     "example.com",
		SrcGeoIP: []string{"US"},
		DstGeoIP: []string{"JP"},
	}
	for i := 0; i < n; i++ {
		var id uuid.UUID
		id[0], id[1], id[2] = byte(i), byte(i>>8), byte(i>>16)
		bt := &benchTracker{}
		bt.info.Metadata = meta
		m.connections.Store(id, bt)
	}
	return m
}

// BenchmarkSnapshotEncodeJSON measures one /connections WebSocket tick per
// client: build the snapshot, then encode it.
func BenchmarkSnapshotEncodeJSON(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		m := benchManager(n)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			buf := &bytes.Buffer{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				if err := json.NewEncoder(buf).Encode(m.Snapshot()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestSnapshotJSONShape pins the wire format of a /connections tick. These
// fields feed the dashboard; the allocation work above must not change a byte.
func TestSnapshotJSONShape(t *testing.T) {
	m := benchManager(2)
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(m.Snapshot()); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{
		`"network":"tcp"`,
		`"type":"Socks5"`,
		`"sourcePort":"51000"`,
		`"destinationPort":"443"`,
		`"host":"example.com"`,
		`"sourceGeoIP":["US"]`,
		`"destinationGeoIP":["JP"]`,
		`"upload":0`,
		`"download":0`,
		`"connections":[`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot JSON missing %s\n%s", want, got)
		}
	}
	// Ports stay strings for dashboard compatibility.
	if strings.Contains(got, `"sourcePort":51000`) {
		t.Fatal("sourcePort must remain a JSON string")
	}
}
