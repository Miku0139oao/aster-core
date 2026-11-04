package v4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The hand-written codec must emit exactly the bytes the previous
// encoding/binary implementation produced, and decode them back identically.
func TestPacketWireFormatUnchanged(t *testing.T) {
	host := "example.com"
	addr := Address{TYPE: AtypDomainName, ADDR: make([]byte, 1+len(host)), PORT: 443}
	addr.ADDR[0] = byte(len(host))
	copy(addr.ADDR[1:], host)

	p := Packet{
		CommandHead: NewCommandHead(PacketType),
		ASSOC_ID:    0x12345678,
		LEN:         4,
		ADDR:        addr,
		DATA:        []byte("data"),
	}

	var got bytes.Buffer
	if err := p.WriteTo(benchWriter{&got}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var want bytes.Buffer
	ref := benchWriter{&want}
	if err := p.CommandHead.WriteTo(ref); err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{p.ASSOC_ID, p.LEN} {
		if err := binary.Write(ref, binary.BigEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := ref.WriteByte(p.ADDR.TYPE); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Write(p.ADDR.ADDR); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(ref, binary.BigEndian, p.ADDR.PORT); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Write(p.DATA); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("wire format changed:\n got %x\nwant %x", got.Bytes(), want.Bytes())
	}
	if got.Len() != p.BytesLen() {
		t.Fatalf("BytesLen = %d, encoded %d", p.BytesLen(), got.Len())
	}

	body := got.Bytes()[2:]
	decoded, err := ReadPacketWithHead(CommandHead{TYPE: PacketType}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ReadPacketWithHead: %v (encoded %x)", err, body)
	}
	if decoded.ASSOC_ID != p.ASSOC_ID || decoded.LEN != p.LEN ||
		decoded.ADDR.PORT != p.ADDR.PORT || !bytes.Equal(decoded.ADDR.ADDR, p.ADDR.ADDR) ||
		!bytes.Equal(decoded.DATA, p.DATA) {
		t.Fatalf("round trip mismatch: %+v vs %+v", decoded, p)
	}
}

type benchWriter struct{ b *bytes.Buffer }

func (w benchWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
func (w benchWriter) WriteByte(c byte) error      { return w.b.WriteByte(c) }
