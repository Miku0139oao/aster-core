package v5

import (
	"bytes"
	"testing"
)

type benchWriter struct{ b *bytes.Buffer }

func (w benchWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
func (w benchWriter) WriteByte(c byte) error      { return w.b.WriteByte(c) }

func benchPacket() Packet {
	host := "example.com"
	addr := Address{TYPE: AtypDomainName, ADDR: make([]byte, 1+len(host)), PORT: 443}
	addr.ADDR[0] = byte(len(host))
	copy(addr.ADDR[1:], host)
	return Packet{
		CommandHead: CommandHead{TYPE: PacketType},
		ASSOC_ID:    1,
		PKT_ID:      2,
		FRAG_TOTAL:  1,
		FRAG_ID:     0,
		SIZE:        32,
		ADDR:        addr,
		DATA:        make([]byte, 32),
	}
}

func BenchmarkPacketWriteTo(b *testing.B) {
	p := benchPacket()
	buf := &bytes.Buffer{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := p.WriteTo(benchWriter{buf}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadPacketWithHead(b *testing.B) {
	p := benchPacket()
	buf := &bytes.Buffer{}
	// The head is written separately from WriteTo, so include it in the fixture.
	if err := p.CommandHead.WriteTo(benchWriter{buf}); err != nil {
		b.Fatal(err)
	}
	if err := p.WriteTo(benchWriter{buf}); err != nil {
		b.Fatal(err)
	}
	encoded := buf.Bytes()
	head := CommandHead{TYPE: PacketType}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReadPacketWithHead(head, bytes.NewReader(encoded)); err != nil {
			b.Fatal(err)
		}
	}
}
