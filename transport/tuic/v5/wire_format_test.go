package v5

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The hand-written codec must emit exactly the bytes the previous
// encoding/binary implementation produced, and decode them back identically.
func TestPacketWireFormatUnchanged(t *testing.T) {
	newPacket := benchPacket()
	newPacket.ASSOC_ID = 0x1234
	newPacket.PKT_ID = 0x5678
	newPacket.FRAG_TOTAL = 2
	newPacket.FRAG_ID = 1
	newPacket.SIZE = 4
	newPacket.DATA = []byte("data")

	var got bytes.Buffer
	if err := newPacket.WriteTo(benchWriter{&got}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Reference: the pre-change encoding, field by field.
	var want bytes.Buffer
	ref := benchWriter{&want}
	if err := newPacket.CommandHead.WriteTo(ref); err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{
		newPacket.ASSOC_ID, newPacket.PKT_ID, newPacket.FRAG_TOTAL,
		newPacket.FRAG_ID, newPacket.SIZE,
	} {
		if err := binary.Write(ref, binary.BigEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := ref.WriteByte(newPacket.ADDR.TYPE); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Write(newPacket.ADDR.ADDR); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(ref, binary.BigEndian, newPacket.ADDR.PORT); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Write(newPacket.DATA); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("wire format changed:\n got %x\nwant %x", got.Bytes(), want.Bytes())
	}
	if got.Len() != newPacket.BytesLen() {
		t.Fatalf("BytesLen = %d, encoded %d", newPacket.BytesLen(), got.Len())
	}

	// And it must round-trip through the hand-written reader. ReadPacketWithHead
	// takes the already-consumed CommandHead, so skip its two bytes.
	body := got.Bytes()[2:]
	decoded, err := ReadPacketWithHead(CommandHead{TYPE: PacketType}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ReadPacketWithHead: %v (encoded %d bytes: %x)", err, len(body), body)
	}
	if decoded.ASSOC_ID != newPacket.ASSOC_ID || decoded.PKT_ID != newPacket.PKT_ID ||
		decoded.FRAG_TOTAL != newPacket.FRAG_TOTAL || decoded.FRAG_ID != newPacket.FRAG_ID ||
		decoded.SIZE != newPacket.SIZE || decoded.ADDR.PORT != newPacket.ADDR.PORT ||
		!bytes.Equal(decoded.ADDR.ADDR, newPacket.ADDR.ADDR) ||
		!bytes.Equal(decoded.DATA, newPacket.DATA) {
		t.Fatalf("round trip mismatch: %+v vs %+v", decoded, newPacket)
	}
}
